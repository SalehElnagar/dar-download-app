#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
evidence_dir="$repo_root/.security/evidence"
component=${IMAGE_COMPONENT:-download}
export PATH="$repo_root/.security/tools/bin:$PATH"
cd "$repo_root"

case "$component" in
  download)
    image_ref=${IMAGE_REF:-dar-download-app:local}
    evidence_prefix=
    expected_entrypoint='["/dar-download"]'
    binary_name=dar-download
    ;;
  worker)
    image_ref=${IMAGE_REF:-dar-distribution-worker:local}
    evidence_prefix=worker-
    expected_entrypoint='["/dar-distribution-worker"]'
    binary_name=dar-distribution-worker
    ;;
  *)
    printf 'unsupported image component: %s\n' "$component" >&2
    exit 1
    ;;
esac
image_marker="$evidence_dir/${evidence_prefix}image.ok"

"$repo_root/scripts/check-tools.sh" >/dev/null
for tool in docker syft trivy grype tar od tr; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'required post-build tool is missing: %s\n' "$tool" >&2
    exit 1
  fi
done
if [[ ! -f "$image_marker" ]]; then
  printf '%s\n' "image evidence is missing; build the current validated source first." >&2
  exit 1
fi

current_source=$("$repo_root/scripts/source-digest.sh")
recorded_source=$(awk -F= '$1 == "source_digest" {print $2}' "$image_marker")
recorded_image=$(awk -F= '$1 == "image_id" {print $2}' "$image_marker")
recorded_platform=$(awk -F= '$1 == "target_platform" {print $2}' "$image_marker")
current_image=$(docker image inspect --format '{{.Id}}' "$image_ref")
current_platform=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image_ref")
if [[ "$recorded_source" != "$current_source" || "$recorded_image" != "$current_image" ]]; then
  printf '%s\n' "source or image changed after construction; rebuild the candidate." >&2
  exit 1
fi
if [[ "$recorded_platform" != "linux/amd64" || "$current_platform" != "$recorded_platform" ]]; then
  printf 'candidate platform mismatch: recorded=%s actual=%s; want linux/amd64.\n' \
    "$recorded_platform" "$current_platform" >&2
  exit 1
fi

printf '%s\n' "[postbuild 1/7] SBOM generation"
syft "$image_ref" --scope all-layers -q \
  -o "spdx-json=$evidence_dir/${evidence_prefix}sbom.spdx.json" \
  -o "cyclonedx-json=$evidence_dir/${evidence_prefix}sbom.cyclonedx.json"

printf '%s\n' "[postbuild 2/7] Trivy image scan"
trivy image \
  --scanners vuln,secret \
  --severity HIGH,CRITICAL \
  --exit-code 1 \
  --format json \
  --output "$evidence_dir/${evidence_prefix}trivy-image.json" \
  "$image_ref"

printf '%s\n' "[postbuild 3/7] Grype image scan"
grype "$image_ref" \
  --fail-on high \
  --output json \
  --file "$evidence_dir/${evidence_prefix}grype-image.json"

printf '%s\n' "[postbuild 4/7] image identity and minimal-content policy"
runtime_user=$(docker image inspect --format '{{.Config.User}}' "$image_ref")
entrypoint=$(docker image inspect --format '{{json .Config.Entrypoint}}' "$image_ref")
if [[ "$runtime_user" != "65532:65532" || "$entrypoint" != "$expected_entrypoint" ]]; then
  printf 'unexpected image user or entrypoint: %s %s\n' "$runtime_user" "$entrypoint" >&2
  exit 1
fi
inspect_container="dar-${component}-inspect-$$"
rootfs_archive="$evidence_dir/${evidence_prefix}image-rootfs.tar"
binary_path="$evidence_dir/${binary_name}.binary"
trap 'docker rm -f "$inspect_container" >/dev/null 2>&1 || true; rm -f "$rootfs_archive" "$binary_path"' EXIT INT TERM
docker create --name "$inspect_container" "$image_ref" >/dev/null
docker export "$inspect_container" >"$rootfs_archive"
tar -tf "$rootfs_archive" >"$evidence_dir/${evidence_prefix}image-files.txt"
tar -xOf "$rootfs_archive" "$binary_name" >"$binary_path"
elf_machine=$(od -An -t x1 -j 18 -N 2 "$binary_path" | tr -d '[:space:]')
docker rm "$inspect_container" >/dev/null
rm -f "$rootfs_archive" "$binary_path"
trap - EXIT INT TERM
if grep -Eq '(^|/)(bin/(ba)?sh|usr/bin/(apt|apt-get|dpkg)|sbin/apk)$' \
  "$evidence_dir/${evidence_prefix}image-files.txt"; then
  printf '%s\n' "image contains a forbidden shell or package manager." >&2
  exit 1
fi
if [[ "$elf_machine" != "3e00" ]]; then
  printf 'application binary has unexpected ELF machine bytes: %s; want AMD64 3e00.\n' \
    "$elf_machine" >&2
  exit 1
fi

if [[ "$component" == download ]]; then
  printf '%s\n' "[postbuild 5/7] read-only container smoke and active DAST"
  IMAGE_REF="$image_ref" "$repo_root/scripts/dast.sh"

  printf '%s\n' "[postbuild 6/7] candidate transfer integrity controls"
  IMAGE_REF="$image_ref" "$repo_root/scripts/test-candidate-transfer.sh"
else
  printf '%s\n' "[postbuild 5/7] no-ingress worker runtime boundary"
  if [[ "$entrypoint" != '["/dar-distribution-worker"]' ]]; then
    printf '%s\n' "worker image does not contain the approved entrypoint." >&2
    exit 1
  fi
  printf '%s\n' "[postbuild 6/7] worker live dependency test deferred to staging"
fi

printf '%s\n' "[postbuild 7/7] final candidate binding"
{
  printf 'source_digest=%s\n' "$current_source"
  printf 'image_id=%s\n' "$current_image"
  printf 'target_platform=%s\n' "$current_platform"
  printf 'completed_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} >"$evidence_dir/${evidence_prefix}postbuild.ok"

printf 'post-build security gates passed for %s\n' "$current_image"
