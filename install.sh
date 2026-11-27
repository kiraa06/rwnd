#!/bin/sh
# Install or update cl from GitHub Releases.
# BINDIR defaults to ~/.local/bin; VERSION defaults to the latest stable tag.
# RWND_REPO can explicitly select another owner/repository.
set -eu

die() {
	printf 'cl installer: %s\n' "$*" >&2
	exit 1
}

fetch() {
	curl -q -fsSL --proto '=https' --proto-redir '=https' \
		--connect-timeout 10 --max-time 120 "$@"
}

check_destination() {
	if [ -L "$dest" ] || { [ -e "$dest" ] && [ ! -f "$dest" ]; }; then
		die "$dest is not a regular file; choose a different BINDIR or use its package manager"
	fi
}

release_repo() {
	if [ -n "${RWND_REPO:-}" ]; then
		printf '%s\n' "$RWND_REPO"
		return
	fi
	# A checked-out installer follows its own origin, not the caller's cwd.
	# Downloads and `curl | sh` have no checkout to disambiguate forks.
	if [ -f "$0" ] && command -v git >/dev/null 2>&1; then
		script_dir=$(CDPATH='' cd -P "$(dirname "$0")" && pwd) || return 1
		if git -C "$script_dir" ls-files --error-unmatch -- "${0##*/}" >/dev/null 2>&1; then
			origin=$(git -C "$script_dir" config --get remote.origin.url) || return 1
			case "$origin" in
			https://github.com/*) slug=${origin#https://github.com/} ;;
			git@github.com:*) slug=${origin#git@github.com:} ;;
			ssh://git@github.com/*) slug=${origin#ssh://git@github.com/} ;;
			*) return 1 ;;
			esac
			printf '%s\n' "${slug%.git}"
			return
		fi
	fi
	printf '%s\n' kiraa06/rwnd
}

main() {
	if [ "$#" -ne 0 ]; then
		case "$1" in
		-h|--help)
			printf '%s\n' 'Install or update cl: sh install.sh' \
				'Overrides: VERSION=vX.Y.Z, BINDIR=~/.local/bin, RWND_REPO=owner/repository'
			return
			;;
		*) die "unknown argument: $1 (see --help)" ;;
		esac
	fi

	repo=$(release_repo) || die 'cannot resolve the GitHub origin of this checkout; set RWND_REPO=owner/repository'
	case "$repo" in *[!A-Za-z0-9_./-]*|*/.|*/..) die 'invalid RWND_REPO; expected owner/repository' ;; esac
	printf '%s\n' "$repo" | grep -Eq '^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$' || die 'invalid RWND_REPO; expected owner/repository'
	releases="https://github.com/$repo/releases"
	bindir=${BINDIR:-$HOME/.local/bin}

	case $(uname -s) in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) die 'supported operating systems: macOS and Linux' ;;
	esac
	case $(uname -m) in
	aarch64|arm64) arch=arm64 ;;
	amd64|x86_64) arch=amd64 ;;
	*) die 'supported architectures: arm64 and amd64' ;;
	esac

	for cmd in curl tar; do
		command -v "$cmd" >/dev/null 2>&1 || die "$cmd is required"
	done
	if command -v sha256sum >/dev/null 2>&1; then
		hasher=sha256sum
	elif command -v shasum >/dev/null 2>&1; then
		hasher=shasum
	else
		die 'sha256sum or shasum is required'
	fi

	version=${VERSION:-}
	if [ -z "$version" ]; then
		latest=$(fetch -o /dev/null -w '%{url_effective}' "$releases/latest") ||
			die "could not query releases for $repo"
		case "$latest" in
		"$releases/tag/"*) version=${latest#"$releases/tag/"} ;;
		"$releases"|"$releases/") die "no stable release published for $repo; build from the checkout or choose a published VERSION" ;;
		*) die "could not determine the latest stable release for $repo" ;;
		esac
	fi
	case "$version" in *[!A-Za-z0-9.-]*) die 'invalid VERSION; expected a tag like v1.2.3' ;; esac
	printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9]+([.-][A-Za-z0-9]+)*)?$' ||
		die "invalid VERSION: $version (expected a tag like v1.2.3)"
	number=${version#v}
	archive="cl_${number}_${os}_${arch}.tar.gz"
	url="$releases/download/$version"

	case "$bindir" in /*) ;; *) bindir="$PWD/$bindir" ;; esac
	mkdir -p "$bindir" || die "cannot create $bindir"
	bindir=$(CDPATH='' cd -P "$bindir" && pwd) || die 'cannot resolve BINDIR'
	dest="$bindir/cl"
	check_destination
	# Stage on the destination filesystem so switching the executable is a rename.
	tmp=$(mktemp -d "$bindir/.cl-install.XXXXXXXX") || die "cannot stage in $bindir"
	trap 'rm -rf "$tmp"' 0
	trap 'exit 129' HUP
	trap 'exit 130' INT
	trap 'exit 143' TERM

	printf 'Fetching cl %s (%s/%s) from %s\n' "$version" "$os" "$arch" "$repo"
	fetch -o "$tmp/checksums.txt" "$url/checksums.txt" || die "cannot download checksums for $repo $version"
	expected=$(awk -v name="$archive" '
		$2 == name || $2 == ("*" name) { hash = $1; count++ }
		END {
			if (count != 1 || length(hash) != 64 || hash !~ /^[[:xdigit:]]+$/) exit 1
			print tolower(hash)
		}' "$tmp/checksums.txt") || die "missing or invalid checksum for $archive"
	fetch -o "$tmp/$archive" "$url/$archive" || die "cannot download $archive from $repo $version"
	if [ "$hasher" = sha256sum ]; then
		digest=$(sha256sum < "$tmp/$archive") || die 'SHA-256 calculation failed'
	else
		digest=$(shasum -a 256 < "$tmp/$archive") || die 'SHA-256 calculation failed'
	fi
	[ "${digest%% *}" = "$expected" ] || die "checksum mismatch for $archive"

	# Stream only cl, rather than extracting files or links into the destination.
	tar -xOzf "$tmp/$archive" cl > "$tmp/cl" || die "could not extract cl from $archive"
	[ -s "$tmp/cl" ] || die "$archive did not contain a binary"
	chmod 0755 "$tmp/cl" || die 'could not make the staged binary executable'
	if [ "$os" = darwin ]; then
		xattr -d com.apple.quarantine "$tmp/cl" 2>/dev/null || :
		codesign --force --sign - "$tmp/cl" >/dev/null || die 'could not sign the staged binary'
	fi
	shown=$("$tmp/cl" --version) || die 'could not run the staged cl binary; existing installation unchanged'
	[ "$shown" = "cl $number" ] || die "unexpected version from the staged binary: $shown"

	check_destination
	if [ -x "$dest" ] && cmp -s "$tmp/cl" "$dest"; then
		printf 'cl %s is already installed at %s\n' "$number" "$dest"
	else
		verb=Installed
		if [ -f "$dest" ]; then verb=Updated; fi
		mv -f "$tmp/cl" "$dest" || die "cannot replace $dest"
		printf '%s cl %s at %s\n' "$verb" "$number" "$dest"
	fi

	resolved=$(command -v cl || :)
	if [ "$resolved" = "$dest" ]; then
		printf 'Run: cl\n'
	elif [ -n "$resolved" ]; then
		printf '%s takes precedence on PATH; run %s directly or reorder PATH.\n' "$resolved" "$dest"
	else
		printf 'Add %s to PATH or run %s directly.\n' "$bindir" "$dest"
	fi
}

main "$@"
