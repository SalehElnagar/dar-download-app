#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
evidence_dir=${EVIDENCE_DIR:-$repo_root/.security/evidence}
repository=${GITHUB_REPOSITORY:-}
revision=${GITHUB_SHA:-}
ref=${GITHUB_REF:-}
run_id=${GITHUB_RUN_ID:-}
run_attempt=${GITHUB_RUN_ATTEMPT:-}
release_tag=${RELEASE_TAG:-}

if [[ ! "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ||
  ! "$revision" =~ ^[a-f0-9]{40}$ ||
  ! "$run_id" =~ ^[1-9][0-9]*$ ||
  ! "$run_attempt" =~ ^[1-9][0-9]*$ ||
  ! "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9]+([.-][A-Za-z0-9]+)*)?$ ||
  "$ref" != "refs/tags/$release_tag" ]]; then
  printf '%s\n' "invalid GitHub release provenance environment." >&2
  exit 1
fi

for file in prebuild.ok image.ok sbom.spdx.json; do
  if [[ ! -f "$evidence_dir/$file" ]]; then
    printf 'required provenance input is missing: %s\n' "$file" >&2
    exit 1
  fi
done

source_digest=$(awk -F= '$1 == "source_digest" {print $2}' "$evidence_dir/prebuild.ok")
image_id=$(awk -F= '$1 == "image_id" {print $2}' "$evidence_dir/image.ok")
if command -v sha256sum >/dev/null 2>&1; then
  sbom_digest=$(sha256sum "$evidence_dir/sbom.spdx.json" | awk '{print $1}')
else
  sbom_digest=$(shasum -a 256 "$evidence_dir/sbom.spdx.json" | awk '{print $1}')
fi
if [[ ! "$source_digest" =~ ^[a-f0-9]{64}$ ||
  ! "$image_id" =~ ^sha256:[a-f0-9]{64}$ ||
  ! "$sbom_digest" =~ ^[a-f0-9]{64}$ ]]; then
  printf '%s\n' "provenance inputs contain an invalid digest." >&2
  exit 1
fi

jq -n \
  --arg build_type "https://github.com/$repository/.github/workflows/release.yaml@v1" \
  --arg builder "https://github.com/$repository/.github/workflows/release.yaml@$ref" \
  --arg invocation "https://github.com/$repository/actions/runs/$run_id/attempts/$run_attempt" \
  --arg repository "git+https://github.com/$repository" \
  --arg revision "$revision" \
  --arg ref "$ref" \
  --arg version "$release_tag" \
  --arg source_digest "$source_digest" \
  --arg image_id "$image_id" \
  --arg sbom_digest "$sbom_digest" \
  '{
    buildDefinition: {
      buildType: $build_type,
      externalParameters: {ref: $ref, version: $version},
      internalParameters: {
        sourceDigest: {sha256: $source_digest},
        candidateImageId: $image_id
      },
      resolvedDependencies: [
        {uri: $repository, digest: {gitCommit: $revision}},
        {
          uri: "https://docker.io/library/golang",
          digest: {sha256: "5978cc992ad5ef96a7469713c8af849c1433824761ce3be2c56381403cd8d9a3"}
        },
        {
          uri: "https://gcr.io/distroless/static-debian13",
          digest: {sha256: "f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6"}
        }
      ]
    },
    runDetails: {
      builder: {id: $builder},
      metadata: {invocationId: $invocation},
      byproducts: [
        {name: "sbom.spdx.json", digest: {sha256: $sbom_digest}}
      ]
    }
  }' >"$evidence_dir/provenance.slsa.json"

jq -e '
  .buildDefinition.buildType != "" and
  (.buildDefinition.resolvedDependencies | length) == 3 and
  .runDetails.builder.id != "" and
  (.runDetails.byproducts | length) == 1
' "$evidence_dir/provenance.slsa.json" >/dev/null

printf '%s\n' "SLSA v1 provenance predicate created for the gated candidate."
