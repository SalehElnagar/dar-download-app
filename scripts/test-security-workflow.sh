#!/usr/bin/env bash
set -Eeuo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

first_digest=$("$repo_root/scripts/source-digest.sh")
second_digest=$("$repo_root/scripts/source-digest.sh")
if [[ "$first_digest" != "$second_digest" || ! "$first_digest" =~ ^[a-f0-9]{64}$ ]]; then
  printf '%s\n' "source digest is not deterministic." >&2
  exit 1
fi
if grep -Eq 'docker (build|buildx)' scripts/prebuild.sh; then
  printf '%s\n' "pre-build gate must not construct an image." >&2
  exit 1
fi
marker_line=$(grep -nF "if [[ ! -f \"\$marker\" ]]" scripts/build-image.sh | cut -d: -f1)
build_line=$(grep -n '^docker build' scripts/build-image.sh | cut -d: -f1)
if [[ -z "$marker_line" || -z "$build_line" || "$marker_line" -ge "$build_line" ]]; then
  printf '%s\n' "image construction is not ordered after evidence validation." >&2
  exit 1
fi
if ! grep -Fq "target_platform=\${TARGET_PLATFORM:-linux/amd64}" scripts/build-image.sh ||
  ! grep -Fq -- "--platform \"\$target_platform\"" scripts/build-image.sh ||
  ! grep -Fq 'target_platform=%s' scripts/build-image.sh; then
  printf '%s\n' "image construction is not explicitly bound to the recorded Linux AMD64 target." >&2
  exit 1
fi
if ! grep -Eq '^FROM golang:[^[:space:]]+@sha256:[0-9a-f]{64} AS build$' Dockerfile ||
  grep -Fq "FROM --platform=\$BUILDPLATFORM" Dockerfile ||
  grep -Eq '^ARG TARGET(OS|ARCH)=' Dockerfile; then
  printf '%s\n' "Dockerfile builder is not portable, digest-pinned, and bound to target arguments." >&2
  exit 1
fi
if ! grep -Fq './scripts/create-provenance.sh' .github/workflows/release.yaml; then
  printf '%s\n' "release workflow does not create source-bound provenance." >&2
  exit 1
fi
if ! grep -Fq './scripts/verify-candidate-transfer.sh' .github/workflows/release.yaml ||
  ! grep -Fq 'scripts/test-candidate-transfer.sh' scripts/postbuild.sh; then
  printf '%s\n' "candidate transfer integrity is not enforced locally and at publish time." >&2
  exit 1
fi
for transfer_check in \
  'postbuild_source=' \
  'postbuild_image=' \
  'actual_platform=' \
  'provenance_image=' \
  'provenance_revision=' \
  'provenance_source=' \
  'provenance_sbom='; do
  if ! grep -Fq "$transfer_check" scripts/verify-candidate-transfer.sh; then
    printf 'release publish job is missing transfer-integrity check: %s\n' "$transfer_check" >&2
    exit 1
  fi
done
postbuild_path_line=$(grep -nF "export PATH=\"\$repo_root/.security/tools/bin:\$PATH\"" scripts/postbuild.sh | cut -d: -f1)
postbuild_scan_line=$(grep -n '^syft ' scripts/postbuild.sh | cut -d: -f1)
if [[ -z "$postbuild_path_line" || -z "$postbuild_scan_line" || "$postbuild_path_line" -ge "$postbuild_scan_line" ]]; then
  printf '%s\n' "post-build scanners are not bound to the repository-pinned tool directory." >&2
  exit 1
fi
if ! grep -Fq "{{.Os}}/{{.Architecture}}" scripts/postbuild.sh ||
  ! grep -Fq 'linux/amd64' scripts/postbuild.sh ||
  ! grep -Fq 'elf_machine' scripts/postbuild.sh ||
  ! grep -Fq '3e00' scripts/postbuild.sh; then
  printf '%s\n' "post-build validation does not enforce the image and binary platform." >&2
  exit 1
fi
if ! grep -Fq "docker network create --internal \"\$network\"" scripts/dast.sh ||
  ! grep -Fq -- '-z "-silent"' scripts/dast.sh ||
  grep -Eq '^[[:space:]]*-(a|I)([[:space:]]|$)' scripts/dast.sh; then
  printf '%s\n' "DAST is not offline, deterministic, and fail-closed on warnings." >&2
  exit 1
fi
# GitHub Actions run 31900648575 exposed a Linux bind-mount permission mismatch.
report_prepare_line=$(grep -nF "install -m 0666 /dev/null \"\$report_file\"" scripts/dast.sh | cut -d: -f1)
report_scan_line=$(grep -n '^[[:space:]]*zap-api-scan\.py' scripts/dast.sh | cut -d: -f1)
report_restore_line=$(grep -n '^[[:space:]]*restore_report_permissions$' scripts/dast.sh | tail -n 1 | cut -d: -f1)
if ! grep -Fq "chmod 0711 \"\$evidence_dir\"" scripts/dast.sh ||
  ! grep -Fq "chmod 0600 \"\${report_files[@]}\"" scripts/dast.sh ||
  ! grep -Fq "chmod 0700 \"\$evidence_dir\"" scripts/dast.sh ||
  [[ -z "$report_prepare_line" || -z "$report_scan_line" || -z "$report_restore_line" ]] ||
  [[ "$report_prepare_line" -ge "$report_scan_line" || "$report_restore_line" -le "$report_scan_line" ]] ||
  [[ $(grep -Ec '^[[:space:]]*restore_report_permissions([[:space:]]|$)' scripts/dast.sh) -lt 2 ]] ||
  grep -Eq 'chmod[[:space:]]+(0777|a\+rwx)' scripts/dast.sh; then
  printf '%s\n' "DAST report files are not narrowly writable during the container scan and restrictive afterward." >&2
  exit 1
fi
if [[ $(grep -c -- '-fuzztime=100000x' scripts/prebuild.sh) != "4" ]] ||
  grep -Eq -- '-fuzztime=[0-9]+s' scripts/prebuild.sh; then
  printf '%s\n' "pre-build fuzzing is not bound to the deterministic iteration budget." >&2
  exit 1
fi

for local_path in .agents .specify specs; do
  tracked_local=$(git ls-files -- "$local_path")
  if [[ -n "$tracked_local" ]]; then
    printf 'local-only operation path is tracked: %s\n' "$local_path" >&2
    exit 1
  fi
  if ! grep -Fxq "$local_path/" .gitignore ||
    ! grep -Fxq "$local_path" .dockerignore ||
    ! grep -Fq -- "--exclude $local_path" scripts/prebuild.sh ||
    ! grep -Fq -- "--skip-dirs $local_path" scripts/prebuild.sh; then
    printf 'local-only operation path is not excluded from product traversal: %s\n' "$local_path" >&2
    exit 1
  fi
done

printf '%s\n' "security workflow order and source digest checks passed."
