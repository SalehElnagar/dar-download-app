#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source_evidence="$repo_root/.security/evidence"
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/dar-download-transfer.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT

for file in prebuild.ok image.ok sbom.spdx.json; do
  cp "$source_evidence/$file" "$fixture_dir/$file"
done
awk -F= '$1 == "source_digest" || $1 == "image_id" || $1 == "target_platform"' \
  "$source_evidence/image.ok" >"$fixture_dir/postbuild.ok"

revision=2222222222222222222222222222222222222222
repository=SalehElnagar/dar-download-app
release_tag=v0.1.0
EVIDENCE_DIR="$fixture_dir" \
  GITHUB_REPOSITORY="$repository" \
  GITHUB_SHA="$revision" \
  GITHUB_REF="refs/tags/$release_tag" \
  GITHUB_RUN_ID=123 \
  GITHUB_RUN_ATTEMPT=1 \
  RELEASE_TAG="$release_tag" \
  "$repo_root/scripts/create-provenance.sh" >/dev/null

EVIDENCE_DIR="$fixture_dir" \
  IMAGE_REF="${IMAGE_REF:-dar-download-app:local}" \
  EXPECTED_REVISION="$revision" \
  EXPECTED_REPOSITORY="$repository" \
  RELEASE_TAG="$release_tag" \
  "$repo_root/scripts/verify-candidate-transfer.sh" >/dev/null

sed 's/^source_digest=.*/source_digest=0000000000000000000000000000000000000000000000000000000000000000/' \
  "$fixture_dir/postbuild.ok" >"$fixture_dir/postbuild.bad"
mv "$fixture_dir/postbuild.bad" "$fixture_dir/postbuild.ok"
if EVIDENCE_DIR="$fixture_dir" \
  IMAGE_REF="${IMAGE_REF:-dar-download-app:local}" \
  EXPECTED_REVISION="$revision" \
  EXPECTED_REPOSITORY="$repository" \
  RELEASE_TAG="$release_tag" \
  "$repo_root/scripts/verify-candidate-transfer.sh" >/dev/null 2>&1; then
  printf '%s\n' "mismatched transfer evidence unexpectedly verified." >&2
  exit 1
fi

printf '%s\n' "candidate transfer positive and negative controls passed."
