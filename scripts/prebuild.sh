#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
evidence_dir="$repo_root/.security/evidence"
tool_bin="$repo_root/.security/tools/bin"
mkdir -p "$evidence_dir"
export PATH="$tool_bin:$PATH"
cd "$repo_root"

if ! command -v gitleaks >/dev/null 2>&1; then
  printf '%s\n' "gitleaks is required and the security workflow cannot continue." >&2
  exit 1
fi

printf '%s\n' "[prebuild 1/14] secret scan"
gitleaks dir . \
  --no-banner \
  --no-color \
  --redact=100 \
  --report-format json \
  --report-path "$evidence_dir/gitleaks-initial.json"

printf '%s\n' "[prebuild 2/14] toolchain validation"
"$repo_root/scripts/check-tools.sh" | tee "$evidence_dir/tool-versions.txt"

printf '%s\n' "[prebuild 3/14] formatting and shell analysis"
unformatted=$(gofmt -l cmd internal)
if [[ -n "$unformatted" ]]; then
  printf 'Go files require formatting:\n%s\n' "$unformatted" >&2
  exit 1
fi
shfmt -d -i 2 -ci scripts
shellcheck scripts/*.sh

printf '%s\n' "[prebuild 4/14] module integrity"
go mod tidy -diff
go mod verify

printf '%s\n' "[prebuild 5/14] compiler and static correctness"
go vet ./...
staticcheck ./...
go build -mod=readonly -trimpath -o "$evidence_dir/dar-download-prebuild" ./cmd/dar-download

printf '%s\n' "[prebuild 6/14] tests and core coverage"
go test -count=1 ./...
go test -count=1 -coverprofile="$evidence_dir/coverage.out" \
  ./internal/auth ./internal/blob ./internal/config ./internal/download \
  ./internal/httpapi ./internal/strictjson
coverage=$(go tool cover -func="$evidence_dir/coverage.out" | awk '/^total:/{gsub(/%/, "", $3); print $3}')
awk -v coverage="$coverage" 'BEGIN { if ((coverage + 0) < 90) exit 1 }'
printf 'core_statement_coverage=%s%%\n' "$coverage" | tee "$evidence_dir/coverage-summary.txt"

printf '%s\n' "[prebuild 7/14] race detector"
go test -count=1 -race ./...

printf '%s\n' "[prebuild 8/14] bounded native fuzzing"
go test -run='^$' -fuzz=FuzzParseEnvironmentPolicy -fuzztime=100000x ./internal/config
go test -run='^$' -fuzz=FuzzAuthenticateOIDCHeaders -fuzztime=100000x ./internal/auth
go test -run='^$' -fuzz=FuzzAuthenticateAzureContainerApps -fuzztime=100000x ./internal/auth
go test -run='^$' -fuzz=FuzzSelectRange -fuzztime=100000x ./internal/download

printf '%s\n' "[prebuild 9/14] Go vulnerability database"
govulncheck ./... | tee "$evidence_dir/govulncheck.txt"

printf '%s\n' "[prebuild 10/14] Go security analysis"
gosec \
  -quiet \
  -severity high \
  -confidence medium \
  -nosec-require-justification \
  -nosec-require-rules \
  -fmt json \
  -out "$evidence_dir/gosec.json" \
  ./...

printf '%s\n' "[prebuild 11/14] repository policy analysis"
"$repo_root/scripts/test-semgrep-rules.sh"
"$repo_root/scripts/run-semgrep.sh" scan \
  --config security/semgrep.yaml \
  --error \
  --metrics=off \
  --exclude .agents \
  --exclude .specify \
  --exclude .security \
  --exclude specs \
  --exclude security/testdata \
  --json \
  --output .security/evidence/semgrep.json \
  .

printf '%s\n' "[prebuild 12/14] filesystem dependency, secret, and configuration scan"
trivy fs \
  --scanners vuln,secret,misconfig \
  --severity HIGH,CRITICAL \
  --exit-code 1 \
  --skip-dirs .agents \
  --skip-dirs .git \
  --skip-dirs .specify \
  --skip-dirs .security \
  --skip-dirs specs \
  --format json \
  --output "$evidence_dir/trivy-fs.json" \
  .

printf '%s\n' "[prebuild 13/14] workflow and gate-order validation"
if compgen -G '.github/workflows/*.yaml' >/dev/null; then
  actionlint .github/workflows/*.yaml
fi
"$repo_root/scripts/test-security-workflow.sh"
"$repo_root/scripts/test-provenance.sh"

printf '%s\n' "[prebuild 14/14] final secret scan"
gitleaks dir . \
  --no-banner \
  --no-color \
  --redact=100 \
  --report-format json \
  --report-path "$evidence_dir/gitleaks-final.json"

source_digest=$("$repo_root/scripts/source-digest.sh")
{
  printf 'source_digest=%s\n' "$source_digest"
  printf 'completed_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'core_statement_coverage=%s\n' "$coverage"
} >"$evidence_dir/prebuild.ok"

printf 'prebuild passed for source %s\n' "$source_digest"
