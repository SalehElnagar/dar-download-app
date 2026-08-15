#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/dar-download-provenance.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT

printf 'source_digest=%064d\n' 0 >"$fixture_dir/prebuild.ok"
printf 'image_id=sha256:%064d\n' 1 >"$fixture_dir/image.ok"
printf '%s\n' '{"spdxVersion":"SPDX-2.3"}' >"$fixture_dir/sbom.spdx.json"

EVIDENCE_DIR="$fixture_dir" \
  GITHUB_REPOSITORY=SalehElnagar/dar-download-app \
  GITHUB_SHA=2222222222222222222222222222222222222222 \
  GITHUB_REF=refs/tags/v0.1.0 \
  GITHUB_RUN_ID=123 \
  GITHUB_RUN_ATTEMPT=1 \
  RELEASE_TAG=v0.1.0 \
  "$repo_root/scripts/create-provenance.sh" >/dev/null

jq -e '
  .buildDefinition.externalParameters.version == "v0.1.0" and
  .buildDefinition.internalParameters.candidateImageId ==
    "sha256:0000000000000000000000000000000000000000000000000000000000000001" and
  .buildDefinition.resolvedDependencies[0].digest.gitCommit ==
    "2222222222222222222222222222222222222222"
' "$fixture_dir/provenance.slsa.json" >/dev/null

if EVIDENCE_DIR="$fixture_dir" \
  GITHUB_REPOSITORY=SalehElnagar/dar-download-app \
  GITHUB_SHA=not-a-revision \
  GITHUB_REF=refs/tags/v0.1.0 \
  GITHUB_RUN_ID=123 \
  GITHUB_RUN_ATTEMPT=1 \
  RELEASE_TAG=v0.1.0 \
  "$repo_root/scripts/create-provenance.sh" >/dev/null 2>&1; then
  printf '%s\n' "invalid provenance input unexpectedly succeeded." >&2
  exit 1
fi

printf '%s\n' "SLSA provenance positive and negative controls passed."
