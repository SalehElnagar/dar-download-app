#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
evidence_dir="$repo_root/.security/evidence"
mkdir -p "$evidence_dir"
result_file="$evidence_dir/semgrep-rules-$$.json"
trap 'rm -f "$result_file"' EXIT

cd "$repo_root"
"$repo_root/scripts/run-semgrep.sh" scan \
  --config security/semgrep.yaml \
  --metrics=off \
  --json \
  --output ".security/evidence/${result_file##*/}" \
  security/testdata

finding_count=$(jq '[.results[] | select(.check_id == "security.dar-no-sensitive-header-logging")] | length' "$result_file")
if [[ "$finding_count" != "2" ]]; then
  printf 'sensitive-header logging rule produced %s findings; expected 2.\n' "$finding_count" >&2
  exit 1
fi

printf '%s\n' "Semgrep security-rule positive and negative controls passed."
