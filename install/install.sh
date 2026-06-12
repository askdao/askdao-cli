#!/usr/bin/env bash
# [INPUT]: 依赖 GitHub Releases API (releases/latest) 与 GoReleaser 资产命名
#          askdao_{ver}_{os}_{arch}.tar.gz + checksums.txt；可选 ASKDAO_VERSION 钉版本
# [OUTPUT]: 安装 askdao 到 ~/.local/bin/askdao（macOS / Linux / WSL）
# [POS]: install/ 的 Unix 安装器，被 https://askdao.ai/install.sh 反向代理分发；
#        与 install.ps1（Windows）逻辑对齐
# [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
#
# Usage:  curl -fsSL https://askdao.ai/install.sh | bash
# Pin:    ASKDAO_VERSION=0.1.0 curl -fsSL https://askdao.ai/install.sh | bash
set -euo pipefail

REPO="askdao/askdao-cli"
INSTALL_DIR="${ASKDAO_INSTALL_DIR:-$HOME/.local/bin}"

say()  { printf '\033[1;32m%s\033[0m\n' "$*"; }
fail() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) fail "unsupported OS: $(uname -s) (Windows PowerShell: irm https://askdao.ai/install.ps1 | iex)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

version="${ASKDAO_VERSION:-}"
if [ -z "$version" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -m1 '"tag_name"' | sed -E 's/.*"v?([^"]+)".*/\1/')"
  [ -n "$version" ] || fail "could not determine the latest version (GitHub API unreachable?)"
fi
version="${version#v}"

asset="askdao_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/v$version"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "Downloading askdao v$version ($os/$arch) ..."
curl -fsSL -o "$tmp/$asset" "$base/$asset"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp" && grep " $asset\$" checksums.txt | sha256sum -c - >/dev/null)
else
  (cd "$tmp" && grep " $asset\$" checksums.txt | shasum -a 256 -c - >/dev/null)
fi

tar -xzf "$tmp/$asset" -C "$tmp" askdao
mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/askdao" "$INSTALL_DIR/askdao"

say "Installed: $INSTALL_DIR/askdao ($("$INSTALL_DIR/askdao" version 2>/dev/null || echo "v$version"))"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    printf '\n%s is not on your PATH. Add it to your shell profile, e.g.:\n' "$INSTALL_DIR"
    printf '  echo '\''export PATH="$HOME/.local/bin:$PATH"'\'' >> ~/.zshrc && source ~/.zshrc\n'
    ;;
esac

say "Next: askdao auth login"
