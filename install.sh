#!/bin/sh
# Install cl, the Claude Code session picker.
#
#   curl -fsSL https://raw.githubusercontent.com/kiraa06/rwnd/main/install.sh | sh
#
# Set BINDIR to choose where the binary lands (default: ~/.local/bin).
# Set VERSION to install a specific tag (default: the latest release).

set -eu

REPO="kiraa06/rwnd"
BINDIR="${BINDIR:-$HOME/.local/bin}"

die() {
	echo "install: $*" >&2
	exit 1
}

os="$(uname -s)"
case "$os" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) die "unsupported operating system: $os (cl supports macOS and Linux)" ;;
esac

arch="$(uname -m)"
case "$arch" in
arm64 | aarch64) arch=arm64 ;;
x86_64 | amd64) arch=amd64 ;;
*) die "unsupported architecture: $arch" ;;
esac

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v tar >/dev/null 2>&1 || die "tar is required"

version="${VERSION:-}"
if [ -z "$version" ]; then
	version="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
		sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)"
	[ -n "$version" ] || die "could not determine the latest release; set VERSION=vX.Y.Z"
fi
number="${version#v}"

archive="cl_${number}_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$version/$archive"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading cl $version ($os/$arch)"
curl -fsSL "$url" -o "$tmp/$archive" || die "download failed: $url"

# Verify against the release checksums when they are published.
if curl -fsSL "https://github.com/$REPO/releases/download/$version/checksums.txt" \
	-o "$tmp/checksums.txt" 2>/dev/null; then
	if command -v shasum >/dev/null 2>&1; then
		expected="$(sed -n "s/ \{1,\}\*\{0,1\}$archive\$//p" "$tmp/checksums.txt" | head -n 1)"
		actual="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
		[ "$expected" = "$actual" ] || die "checksum mismatch for $archive"
		echo "Checksum verified"
	fi
fi

tar -xzf "$tmp/$archive" -C "$tmp" cl || die "could not extract cl from $archive"
mkdir -p "$BINDIR"
mv "$tmp/cl" "$BINDIR/cl"
chmod +x "$BINDIR/cl"

if [ "$os" = darwin ]; then
	# Go ships linker-signed binaries. Once one has been downloaded, Gatekeeper
	# rejects it and the kernel kills it on launch ("killed: 9"), so clear the
	# download marker and re-sign it ad hoc.
	xattr -c "$BINDIR/cl" 2>/dev/null || true
	if command -v codesign >/dev/null 2>&1; then
		codesign --force --sign - "$BINDIR/cl" >/dev/null 2>&1 ||
			echo "warning: could not re-sign the binary; if it is killed on launch, run:
  codesign --force --sign - $BINDIR/cl" >&2
	fi
fi

echo "Installed $BINDIR/cl"

case ":$PATH:" in
*":$BINDIR:"*) echo "Run: cl" ;;
*)
	echo
	echo "$BINDIR is not on your PATH. Add it:"
	echo "  echo 'export PATH=\"$BINDIR:\$PATH\"' >> ~/.zshrc && exec zsh"
	;;
esac
