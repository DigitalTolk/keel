#!/usr/bin/env bash
#
# keel installer. Downloads the right release binary for this OS/arch from
# GitHub Releases, verifies its SHA256 against the release checksums, and
# installs it. No authentication required (public repo).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/DigitalTolk/keel/main/install.sh | bash
#
# Environment overrides:
#   KEEL_VERSION   release tag to install, e.g. v1.2.0 (default: latest)
#   KEEL_BIN_DIR   install dir (default: /usr/local/bin, or ~/.local/bin without root)
#
set -euo pipefail

OWNER="DigitalTolk"
REPO="keel"

log()  { printf '%s\n' "keel-install: $*" >&2; }
die()  { log "ERROR: $*"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "'$1' is required"; }

need curl
need tar
command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1 || die "sha256sum or shasum is required"

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# --- detect platform ---------------------------------------------------------
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${os}" in
  linux|darwin) ;;
  *) die "unsupported OS: ${os}" ;;
esac

arch="$(uname -m)"
case "${arch}" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) die "unsupported arch: ${arch}" ;;
esac

# --- resolve version (tag) ---------------------------------------------------
tag="${KEEL_VERSION:-}"
if [ -z "${tag}" ]; then
  log "resolving latest release"
  tag="$(curl -fsSL "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  [ -n "${tag}" ] || die "could not resolve latest release; set KEEL_VERSION (e.g. v1.2.0)"
fi
version="${tag#v}"   # GoReleaser strips the leading v in asset names
log "installing keel ${tag} (${os}/${arch})"

# --- download + verify -------------------------------------------------------
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

asset="keel_${version}_${os}_${arch}.tar.gz"
base="https://github.com/${OWNER}/${REPO}/releases/download/${tag}"

curl -fsSL "${base}/${asset}"      -o "${tmp}/${asset}"      || die "download ${asset} failed"
curl -fsSL "${base}/checksums.txt" -o "${tmp}/checksums.txt" || die "download checksums failed"

expected="$(grep " ${asset}\$" "${tmp}/checksums.txt" | awk '{print $1}')"
[ -n "${expected}" ] || die "no checksum entry for ${asset}"
got="$(sha256_of "${tmp}/${asset}")"
[ "${got}" = "${expected}" ] || die "checksum mismatch for ${asset} (got ${got}, want ${expected})"
log "checksum OK"

tar -xzf "${tmp}/${asset}" -C "${tmp}"

# --- install -----------------------------------------------------------------
bindir="${KEEL_BIN_DIR:-}"
if [ -z "${bindir}" ]; then
  if [ "$(id -u)" = "0" ]; then bindir="/usr/local/bin"; else bindir="${HOME}/.local/bin"; fi
fi
mkdir -p "${bindir}"
install -m 0755 "${tmp}/keel" "${bindir}/keel"

log "installed to ${bindir}/keel"
"${bindir}/keel" --version || true
