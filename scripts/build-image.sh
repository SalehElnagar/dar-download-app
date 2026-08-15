#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
evidence_dir="$repo_root/.security/evidence"
marker="$evidence_dir/prebuild.ok"
image_ref=${IMAGE_REF:-dar-download-app:local}
version=${VERSION:-dev}
target_platform=${TARGET_PLATFORM:-linux/amd64}
cd "$repo_root"

if [[ ! -f "$marker" ]]; then
  printf '%s\n' "current pre-build evidence is required before image construction." >&2
  exit 1
fi
current_digest=$("$repo_root/scripts/source-digest.sh")
recorded_digest=$(awk -F= '$1 == "source_digest" {print $2}' "$marker")
if [[ -z "$recorded_digest" || "$recorded_digest" != "$current_digest" ]]; then
  printf '%s\n' "source changed after pre-build validation; run the pre-build gate again." >&2
  exit 1
fi
if [[ ! "$image_ref" =~ ^[A-Za-z0-9._/@:-]+$ || ! "$version" =~ ^[A-Za-z0-9._+-]{1,64}$ ]]; then
  printf '%s\n' "image reference or version is invalid." >&2
  exit 1
fi
if [[ "$target_platform" != "linux/amd64" ]]; then
  printf 'unsupported target platform: %s; only linux/amd64 is release-approved.\n' "$target_platform" >&2
  exit 1
fi

revision=$(git rev-parse --verify HEAD 2>/dev/null || printf '%s' "$current_digest")
docker build \
  --platform "$target_platform" \
  --pull \
  --no-cache \
  --provenance=false \
  --sbom=false \
  --build-arg "VERSION=$version" \
  --build-arg "REVISION=$revision" \
  --iidfile "$evidence_dir/image.iid" \
  --tag "$image_ref" \
  .

image_id=$(docker image inspect --format '{{.Id}}' "$image_ref")
{
  printf 'source_digest=%s\n' "$current_digest"
  printf 'image_ref=%s\n' "$image_ref"
  printf 'image_id=%s\n' "$image_id"
  printf 'target_platform=%s\n' "$target_platform"
} >"$evidence_dir/image.ok"

printf 'image built: %s (%s)\n' "$image_ref" "$image_id"
