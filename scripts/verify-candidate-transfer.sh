#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

evidence_dir=${EVIDENCE_DIR:?EVIDENCE_DIR is required}
image_ref=${IMAGE_REF:?IMAGE_REF is required}
expected_revision=${EXPECTED_REVISION:?EXPECTED_REVISION is required}
expected_repository=${EXPECTED_REPOSITORY:?EXPECTED_REPOSITORY is required}
release_tag=${RELEASE_TAG:?RELEASE_TAG is required}

if [[ ! "$image_ref" =~ ^[A-Za-z0-9][A-Za-z0-9._/@:-]*$ ||
  ! "$expected_revision" =~ ^[a-f0-9]{40}$ ||
  ! "$expected_repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ||
  ! "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9]+([.-][A-Za-z0-9]+)*)?$ ]]; then
  printf '%s\n' "candidate verification inputs are invalid." >&2
  exit 1
fi

required=(prebuild.ok image.ok postbuild.ok sbom.spdx.json provenance.slsa.json)
for file in "${required[@]}"; do
  if [[ ! -f "$evidence_dir/$file" ]]; then
    printf 'transferred candidate is missing required evidence: %s\n' "$file" >&2
    exit 1
  fi
done

marker_value() {
  local file=$1
  local key=$2
  awk -F= -v key="$key" '
    $1 == key { value = $2; count += 1 }
    END { if (count != 1) exit 1; print value }
  ' "$file"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -- "$1" | awk '{print $1}'
  else
    shasum -a 256 -- "$1" | awk '{print $1}'
  fi
}

prebuild_source=$(marker_value "$evidence_dir/prebuild.ok" source_digest)
image_source=$(marker_value "$evidence_dir/image.ok" source_digest)
postbuild_source=$(marker_value "$evidence_dir/postbuild.ok" source_digest)
expected=$(marker_value "$evidence_dir/image.ok" image_id)
postbuild_image=$(marker_value "$evidence_dir/postbuild.ok" image_id)
image_platform=$(marker_value "$evidence_dir/image.ok" target_platform)
postbuild_platform=$(marker_value "$evidence_dir/postbuild.ok" target_platform)
actual=$(docker image inspect --format '{{.Id}}' "$image_ref")
actual_platform=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image_ref")
actual_user=$(docker image inspect --format '{{.Config.User}}' "$image_ref")

provenance_image=$(jq -er '.buildDefinition.internalParameters.candidateImageId' "$evidence_dir/provenance.slsa.json")
provenance_revision=$(jq -er '.buildDefinition.resolvedDependencies[0].digest.gitCommit' "$evidence_dir/provenance.slsa.json")
provenance_repository=$(jq -er '.buildDefinition.resolvedDependencies[0].uri' "$evidence_dir/provenance.slsa.json")
provenance_source=$(jq -er '.buildDefinition.internalParameters.sourceDigest.sha256' "$evidence_dir/provenance.slsa.json")
provenance_ref=$(jq -er '.buildDefinition.externalParameters.ref' "$evidence_dir/provenance.slsa.json")
provenance_version=$(jq -er '.buildDefinition.externalParameters.version' "$evidence_dir/provenance.slsa.json")
provenance_sbom=$(jq -er '.runDetails.byproducts[0].digest.sha256' "$evidence_dir/provenance.slsa.json")
sbom_digest=$(sha256_file "$evidence_dir/sbom.spdx.json")

if [[ ! "$prebuild_source" =~ ^[a-f0-9]{64}$ ||
  "$image_source" != "$prebuild_source" ||
  "$postbuild_source" != "$prebuild_source" ||
  "$provenance_source" != "$prebuild_source" ||
  ! "$expected" =~ ^sha256:[a-f0-9]{64}$ ||
  "$postbuild_image" != "$expected" ||
  "$provenance_image" != "$expected" ||
  "$actual" != "$expected" ||
  "$image_platform" != 'linux/amd64' ||
  "$postbuild_platform" != 'linux/amd64' ||
  "$actual_platform" != 'linux/amd64' ||
  "$actual_user" != '65532:65532' ||
  "$provenance_revision" != "$expected_revision" ||
  "$provenance_repository" != "git+https://github.com/$expected_repository" ||
  "$provenance_ref" != "refs/tags/$release_tag" ||
  "$provenance_version" != "$release_tag" ||
  "$provenance_sbom" != "$sbom_digest" ]]; then
  printf '%s\n' "transferred image, evidence, or provenance identity is inconsistent." >&2
  exit 1
fi

printf '%s\n' "candidate transfer identity verified."
