#!/usr/bin/env bash
set -Eeuo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
semgrep_image=semgrep/semgrep@sha256:67319956da3dcb58baf5b322899c15458e3963e7018a86aeeb5cd224e69cb77a

if command -v semgrep >/dev/null 2>&1 && semgrep --version 2>/dev/null | tail -1 | grep -Fx '1.173.0' >/dev/null; then
  exec semgrep "$@"
fi

mkdir -p "$repo_root/.security/evidence"
exec docker run --rm \
  --network none \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --user "$(id -u):$(id -g)" \
  --env HOME=/tmp \
  --env SEMGREP_ENABLE_VERSION_CHECK=0 \
  --env SEMGREP_SEND_METRICS=off \
  --tmpfs /tmp:rw,nosuid,nodev \
  --volume "$repo_root:/workspace:ro" \
  --volume "$repo_root/.security/evidence:/workspace/.security/evidence:rw" \
  --workdir /workspace \
  "$semgrep_image" semgrep "$@"
