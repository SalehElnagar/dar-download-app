#!/usr/bin/env bash
set -Eeuo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
export PATH="$repo_root/.security/tools/bin:$PATH"

required=(
  go docker gitleaks trivy grype syft shellcheck shfmt curl jq tar
  staticcheck govulncheck gosec
)
for tool in "${required[@]}"; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'required tool is missing: %s\n' "$tool" >&2
    exit 1
  fi
done

if [[ $(go version) != *"go1.26.6"* ]]; then
  printf 'Go 1.26.6 is required; found: %s\n' "$(go version)" >&2
  exit 1
fi
staticcheck -version | grep -F 'v0.7.0' >/dev/null
govulncheck -version | grep -F 'govulncheck@v1.7.0' >/dev/null
go version -m "$(command -v gosec)" |
  grep -F $'\tmod\tgithub.com/securego/gosec/v2\tv2.28.0\t' >/dev/null
gitleaks version | grep -Fx '8.30.1' >/dev/null
trivy --version | head -1 | grep -F '0.74.0' >/dev/null
grype version | awk -F: '/Version/{gsub(/ /,"",$2); print $2; exit}' | grep -Fx '0.117.0' >/dev/null
syft version | awk -F: '/Version/{gsub(/ /,"",$2); print $2; exit}' | grep -Fx '1.51.0' >/dev/null
shellcheck --version | awk '/^version:/{print $2}' | grep -Fx '0.11.0' >/dev/null
shfmt --version | grep -Ex 'v?3[.]13[.]1' >/dev/null
"$repo_root/scripts/run-semgrep.sh" --version | tail -1 | grep -Fx '1.173.0' >/dev/null
docker info >/dev/null

printf 'go=%s\n' "$(go version)"
printf 'gitleaks=%s\n' "$(gitleaks version)"
printf 'semgrep=%s\n' "$("$repo_root/scripts/run-semgrep.sh" --version 2>/dev/null | tail -1)"
printf 'trivy=%s\n' "$(trivy --version | head -1)"
printf 'grype=%s\n' "$(grype version | awk -F: '/Version/{gsub(/ /,"",$2); print $2; exit}')"
printf 'syft=%s\n' "$(syft version | awk -F: '/Version/{gsub(/ /,"",$2); print $2; exit}')"
