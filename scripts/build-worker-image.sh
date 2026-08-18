#!/usr/bin/env bash
set -Eeuo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
export IMAGE_COMPONENT=worker
export IMAGE_REF=${WORKER_IMAGE_REF:-dar-distribution-worker:local}
exec "$repo_root/scripts/build-image.sh"
