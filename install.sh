#!/bin/sh
# loack installer — download the loack core from the latest GitHub release,
# verify its checksum, and install it.
#
#   curl -fsSL https://raw.githubusercontent.com/chanwit/loack/main/install.sh | sh
#
# Configure with environment variables:
#   LOACK_INSTALL_DIR   where to install   (default: $HOME/bin)
#   LOACK_VERSION       release tag        (default: latest)
#   LOACK_VARIANT       loack | loack-aio  (default: loack — the core)
#   LOACK_REPO          GitHub repo        (default: chanwit/loack)
#
# Examples:
#   curl -fsSL .../install.sh | sh
#   curl -fsSL .../install.sh | LOACK_INSTALL_DIR=/usr/local/bin sh
#   curl -fsSL .../install.sh | LOACK_VERSION=v0.1.1 LOACK_VARIANT=loack-aio sh
set -eu

REPO="${LOACK_REPO:-chanwit/loack}"
VARIANT="${LOACK_VARIANT:-loack}"
DIR="${LOACK_INSTALL_DIR:-$HOME/bin}"

info() { printf 'loack-install: %s\n' "$*" >&2; }
die() {
	printf 'loack-install: error: %s\n' "$*" >&2
	exit 1
}
have() { command -v "$1" >/dev/null 2>&1; }

dl() { # url -> stdout
	if have curl; then curl -fsSL "$1"; else wget -qO- "$1"; fi
}
dlf() { # url file
	if have curl; then curl -fsSL -o "$2" "$1"; else wget -qO "$2" "$1"; fi
}

have curl || have wget || die "need curl or wget"
case "$VARIANT" in loack | loack-aio) ;; *) die "LOACK_VARIANT must be loack or loack-aio" ;; esac

# --- platform ---
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in linux | darwin) ;; *) die "unsupported OS: $os (loack publishes linux and darwin)" ;; esac
arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported arch: $arch (loack publishes amd64 and arm64)" ;;
esac

# --- version: explicit, else resolve "latest" via the releases redirect ---
ver="${LOACK_VERSION:-}"
if [ -z "$ver" ]; then
	info "resolving the latest release of $REPO..."
	ver=$(dl "https://api.github.com/repos/$REPO/releases/latest" |
		grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name"[ ]*:[ ]*"([^"]+)".*/\1/')
	[ -n "$ver" ] || die "could not resolve the latest release of $REPO"
fi

asset="${VARIANT}_${ver}_${os}_${arch}"
base="https://github.com/$REPO/releases/download/$ver"
info "installing $VARIANT $ver ($os/$arch)"

# --- download + verify in a temp dir ---
tmp=$(mktemp -d 2>/dev/null || mktemp -d -t loack)
trap 'rm -rf "$tmp"' EXIT INT TERM

dlf "$base/$asset" "$tmp/$asset" || die "downloading $asset (is $os/$arch published in $ver?)"

sums=$(dl "$base/SHA256SUMS") || die "downloading SHA256SUMS for $ver"
want=$(printf '%s\n' "$sums" | awk -v a="$asset" '$2==a{print $1}')
[ -n "$want" ] || die "no checksum for $asset in release $ver"

if have sha256sum; then
	got=$(sha256sum "$tmp/$asset" | awk '{print $1}')
elif have shasum; then
	got=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
else
	die "need sha256sum or shasum to verify the download"
fi
[ "$got" = "$want" ] || die "checksum mismatch for $asset (got $got, want $want)"

# --- install ---
dest="$DIR/$VARIANT"
mkdir -p "$DIR"
if have install; then
	install -m 0755 "$tmp/$asset" "$dest"
else
	cp "$tmp/$asset" "$dest" && chmod 0755 "$dest"
fi
info "installed $VARIANT -> $dest"

case ":$PATH:" in
*":$DIR:"*) ;;
*) info "note: $DIR is not on your PATH — add:  export PATH=\"$DIR:\$PATH\"" ;;
esac

"$dest" --version 2>/dev/null || true
