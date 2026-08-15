#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
evidence_dir="$repo_root/.security/evidence"
image_ref=${IMAGE_REF:-dar-download-app:local}
suffix=$$
network="dar-download-dast-$suffix"
app_container="dar-download-dast-app-$suffix"
zap_image="ghcr.io/zaproxy/zaproxy@sha256:781a2bdaea47324e7bab583e2263f21d257b0aee61ed51521a5be45f5f5081ef"
release_policy='{"dar_01JABCDEF0123456789XYZ":{"allowed_principal_ids":["33333333-3333-4333-8333-333333333333"],"blob_name":"releases/synthetic/example.dar","download_name":"example.dar"}}'

cleanup() {
  docker rm -f "$app_container" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

mkdir -p "$evidence_dir"
docker image inspect "$image_ref" >/dev/null
docker network create --internal "$network" >/dev/null
docker run -d \
  --name "$app_container" \
  --network "$network" \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --pids-limit 128 \
  --memory 128m \
  --cpus 1 \
  --env HARMONY_DAR_TENANT_ID=11111111-1111-4111-8111-111111111111 \
  --env HARMONY_DAR_STORAGE_ACCOUNT_NAME=stdardownloadpoc01 \
  --env HARMONY_DAR_STORAGE_CONTAINER=dar-releases \
  --env HARMONY_DAR_MANAGED_IDENTITY_CLIENT_ID=22222222-2222-4222-8222-222222222222 \
  --env "HARMONY_DAR_RELEASES_JSON=$release_policy" \
  "$image_ref" >/dev/null

target_url="http://$app_container:8000"
for _ in $(seq 1 30); do
  if docker run --rm \
    --network "$network" \
    --entrypoint curl \
    "$zap_image" \
    --fail --silent --show-error "$target_url/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker run --rm \
  --network "$network" \
  --entrypoint curl \
  "$zap_image" \
  --fail --silent --show-error "$target_url/healthz" >/dev/null
status=$(docker run --rm \
  --network "$network" \
  --entrypoint curl \
  "$zap_image" \
  --silent --output /dev/null --write-out '%{http_code}' \
  "$target_url/v1/releases/dar_01JABCDEF0123456789XYZ/download")
if [[ "$status" != "401" ]]; then
  printf 'unauthenticated container response was %s, want 401\n' "$status" >&2
  exit 1
fi

docker run --rm \
  --network "$network" \
  --volume "$repo_root/api:/zap/api:ro" \
  --volume "$repo_root/security:/zap/security:ro" \
  --volume "$evidence_dir:/zap/wrk:rw" \
  "$zap_image" \
  zap-api-scan.py \
  -t /zap/api/openapi.yaml \
  -f openapi \
  -O "$target_url" \
  -c /zap/security/zap-rules.tsv \
  -z "-silent" \
  -T 5 \
  -J zap-report.json \
  -r zap-report.html \
  -w zap-report.md \
  -s

printf '%s\n' "local DAST completed without a release-blocking finding."
