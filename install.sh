#!/bin/sh
# Installs one pinned release. Download and review this script before running it.
set -eu
umask 077

VERSION='0.2.2'
REPOSITORY='liuyang1520/ma'
prefix=${HOME:?HOME must be set}/.local
work_dir=''
staged_binary=''

die() { printf 'ma installer: %s\n' "$*" >&2; exit 1; }
cleanup() {
    [ -z "$staged_binary" ] || rm -f "$staged_binary"
    [ -z "$work_dir" ] || rm -rf "$work_dir"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

while [ "$#" -gt 0 ]; do
    case "$1" in
        --prefix)
            [ "$#" -ge 2 ] || die '--prefix needs a directory'
            prefix=$2
            shift 2
            ;;
        --help|-h)
            printf 'Usage: sh install.sh [--prefix /absolute/path]\nInstalls ma %s to PREFIX/bin/ma (default: ~/.local).\n' "$VERSION"
            exit 0
            ;;
        *) die "unknown argument: $1" ;;
    esac
done
case "$prefix" in /*) ;; *) die '--prefix must be an absolute path' ;; esac

case "$(uname -s)" in
    Darwin) target_os=darwin ;;
    Linux) target_os=linux ;;
    *) die 'supported systems: macOS and Linux' ;;
esac
case "$(uname -m)" in
    x86_64|amd64) target_arch=amd64 ;;
    arm64|aarch64) target_arch=arm64 ;;
    *) die 'supported architectures: amd64 and arm64' ;;
esac
for tool in curl tar awk mktemp install mv; do
    command -v "$tool" >/dev/null 2>&1 || die "required command not found: $tool"
done
if command -v sha256sum >/dev/null 2>&1; then
    hash_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then
    hash_tool=shasum
else
    die 'sha256sum or shasum is required to verify the download'
fi

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/ma-install.XXXXXXXX")
asset="ma_${VERSION}_${target_os}_${target_arch}.tar.gz"
base="https://github.com/${REPOSITORY}/releases/download/v${VERSION}"
download() {
    # Ignore .curlrc and forbid redirects to unencrypted protocols.
    curl --disable --fail --silent --show-error --location \
        --proto '=https' --proto-redir '=https' --tlsv1.2 \
        --connect-timeout 15 --max-time 180 --retry 2 \
        --output "$work_dir/$1" "$base/$1"
}
download SHA256SUMS
download "$asset"
expected=$(awk -v name="$asset" '
    $2 == name { count++; digest = $1 }
    END {
        if (count != 1 || length(digest) != 64 || digest ~ /[^0-9a-f]/) exit 1
        print digest
    }' "$work_dir/SHA256SUMS") || die 'missing or invalid SHA-256 checksum'
if [ "$hash_tool" = sha256sum ]; then
    actual=$(sha256sum "$work_dir/$asset")
else
    actual=$(shasum -a 256 "$work_dir/$asset")
fi
[ "${actual%% *}" = "$expected" ] || die 'SHA-256 mismatch; nothing installed'

# Read only named members into private files. Never extract archive paths to disk.
tar -tzf "$work_dir/$asset" > "$work_dir/members"
awk '
    $0 != "ma" && $0 != "README.md" && $0 != "THIRD_PARTY_NOTICES.txt" && $0 != "LICENSE" { exit 1 }
    { if (++seen[$0] != 1) exit 1 }
    END { if (!seen["ma"] || !seen["README.md"] || !seen["THIRD_PARTY_NOTICES.txt"]) exit 1 }
    ' "$work_dir/members" || die 'unexpected archive contents'
tar -xzOf "$work_dir/$asset" ma > "$work_dir/ma"
tar -xzOf "$work_dir/$asset" THIRD_PARTY_NOTICES.txt > "$work_dir/THIRD_PARTY_NOTICES.txt"
[ -s "$work_dir/ma" ] && [ -s "$work_dir/THIRD_PARTY_NOTICES.txt" ] || die 'incomplete archive'
if awk '$0 == "LICENSE" { found = 1 } END { exit !found }' "$work_dir/members"; then
    tar -xzOf "$work_dir/$asset" LICENSE > "$work_dir/LICENSE"
fi

[ ! -d "$prefix/bin/ma" ] || die "$prefix/bin/ma is a directory"
install -d -m 755 "$prefix/bin" "$prefix/share/doc/ma"
install -m 644 "$work_dir/THIRD_PARTY_NOTICES.txt" "$prefix/share/doc/ma/THIRD_PARTY_NOTICES.txt"
if [ -f "$work_dir/LICENSE" ]; then
    install -m 644 "$work_dir/LICENSE" "$prefix/share/doc/ma/LICENSE"
fi
staged_binary=$(mktemp "$prefix/bin/.ma-install.XXXXXXXX")
install -m 755 "$work_dir/ma" "$staged_binary"
mv -f "$staged_binary" "$prefix/bin/ma"
staged_binary=''
printf 'Installed ma %s to %s/bin/ma\n' "$VERSION" "$prefix"
printf 'Ensure %s/bin is on your PATH, then run: ma file.md\n' "$prefix"
