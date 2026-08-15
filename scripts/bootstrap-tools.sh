#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tool_bin="$repo_root/.security/tools/bin"
scratch_dir=$(mktemp -d "${TMPDIR:-/tmp}/dar-download-tools.XXXXXX")
trap 'rm -rf "$scratch_dir"' EXIT
mkdir -p "$tool_bin"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -- "$1" | awk '{print $1}'
  else
    shasum -a 256 -- "$1" | awk '{print $1}'
  fi
}

download_verified() {
  local url=$1
  local expected=$2
  local destination=$3
  curl --proto '=https' --tlsv1.2 --fail --silent --show-error --location \
    --output "$destination" "$url"
  local actual
  actual=$(sha256_file "$destination")
  if [[ "$actual" != "$expected" ]]; then
    printf 'checksum mismatch for %s\n' "$url" >&2
    exit 1
  fi
}

os=$(uname -s)
arch=$(uname -m)
case "$os/$arch" in
  Linux/x86_64)
    gitleaks_asset=gitleaks_8.30.1_linux_x64.tar.gz
    gitleaks_sha=551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb
    trivy_asset=trivy_0.74.0_Linux-64bit.tar.gz
    trivy_sha=2ae6fe3ee734b7fdf11335663e18c75ea12dccc76062f09f164a3b0f8be4371a
    grype_asset=grype_0.117.0_linux_amd64.tar.gz
    grype_sha=38525dab1e06f162ebaa02f94d82d1f807076b011a44180cf2777edf1a7b9c26
    syft_asset=syft_1.51.0_linux_amd64.tar.gz
    syft_sha=2a2e837a2c8d59ec9af5472ee22d3b04ee463c4e44476ecf993fd1e5ab6ebc7f
    shfmt_asset=shfmt_v3.13.1_linux_amd64
    shfmt_sha=fb096c5d1ac6beabbdbaa2874d025badb03ee07929f0c9ff67563ce8c75398b1
    shellcheck_asset=shellcheck-v0.11.0.linux.x86_64.tar.xz
    shellcheck_sha=8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198
    ;;
  Darwin/arm64)
    gitleaks_asset=gitleaks_8.30.1_darwin_arm64.tar.gz
    gitleaks_sha=b40ab0ae55c505963e365f271a8d3846efbc170aa17f2607f13df610a9aeb6a5
    trivy_asset=trivy_0.74.0_macOS-ARM64.tar.gz
    trivy_sha=1caada5e0e2091909357c7525d3aa76f4b660b13821bc143b190c7483e31cc11
    grype_asset=grype_0.117.0_darwin_arm64.tar.gz
    grype_sha=bfcefa3f3b1690d9c77d847841b32ebd6106ab0e0e32f810924707e704d53584
    syft_asset=syft_1.51.0_darwin_arm64.tar.gz
    syft_sha=4f37f4c7fefce0a68e4cf71ba3f5f9829a99e65d89b29f7ee41b8c2c10ea8c59
    shfmt_asset=shfmt_v3.13.1_darwin_arm64
    shfmt_sha=9680526be4a66ea1ffe988ed08af58e1400fe1e4f4aef5bd88b20bb9b3da33f8
    shellcheck_asset=shellcheck-v0.11.0.darwin.aarch64.tar.xz
    shellcheck_sha=56affdd8de5527894dca6dc3d7e0a99a873b0f004d7aabc30ae407d3f48b0a79
    ;;
  *)
    printf 'unsupported security-tool platform: %s/%s\n' "$os" "$arch" >&2
    exit 1
    ;;
esac

download_verified "https://github.com/zricethezav/gitleaks/releases/download/v8.30.1/$gitleaks_asset" "$gitleaks_sha" "$scratch_dir/gitleaks.tar.gz"
tar -xzf "$scratch_dir/gitleaks.tar.gz" -C "$scratch_dir"
install -m 0755 "$scratch_dir/gitleaks" "$tool_bin/gitleaks"

download_verified "https://github.com/aquasecurity/trivy/releases/download/v0.74.0/$trivy_asset" "$trivy_sha" "$scratch_dir/trivy.tar.gz"
tar -xzf "$scratch_dir/trivy.tar.gz" -C "$scratch_dir"
install -m 0755 "$scratch_dir/trivy" "$tool_bin/trivy"

download_verified "https://github.com/anchore/grype/releases/download/v0.117.0/$grype_asset" "$grype_sha" "$scratch_dir/grype.tar.gz"
tar -xzf "$scratch_dir/grype.tar.gz" -C "$scratch_dir"
install -m 0755 "$scratch_dir/grype" "$tool_bin/grype"

download_verified "https://github.com/anchore/syft/releases/download/v1.51.0/$syft_asset" "$syft_sha" "$scratch_dir/syft.tar.gz"
tar -xzf "$scratch_dir/syft.tar.gz" -C "$scratch_dir"
install -m 0755 "$scratch_dir/syft" "$tool_bin/syft"

download_verified "https://github.com/mvdan/sh/releases/download/v3.13.1/$shfmt_asset" "$shfmt_sha" "$scratch_dir/shfmt"
install -m 0755 "$scratch_dir/shfmt" "$tool_bin/shfmt"

download_verified "https://github.com/koalaman/shellcheck/releases/download/v0.11.0/$shellcheck_asset" "$shellcheck_sha" "$scratch_dir/shellcheck.tar.xz"
tar -xJf "$scratch_dir/shellcheck.tar.xz" -C "$scratch_dir"
install -m 0755 "$scratch_dir/shellcheck-v0.11.0/shellcheck" "$tool_bin/shellcheck"

export GOBIN="$tool_bin"
go install honnef.co/go/tools/cmd/staticcheck@v0.7.0
go install golang.org/x/vuln/cmd/govulncheck@v1.7.0
go install github.com/securego/gosec/v2/cmd/gosec@v2.28.0
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

docker pull semgrep/semgrep@sha256:67319956da3dcb58baf5b322899c15458e3963e7018a86aeeb5cd224e69cb77a >/dev/null
docker pull ghcr.io/zaproxy/zaproxy@sha256:781a2bdaea47324e7bab583e2263f21d257b0aee61ed51521a5be45f5f5081ef >/dev/null

printf '%s\n' "Pinned security tools installed in .security/tools/bin."
