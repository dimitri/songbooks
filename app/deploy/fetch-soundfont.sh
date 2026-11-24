#!/bin/sh
# fetch-soundfont.sh [dir]: fetches GeneralUser GS (the General MIDI bank
# Guitar Pro scores play with, docs/soundfont.md) and its license into
# dir, checked against the SHA-256s deploy/Dockerfile pins — the same
# files the production image fetches, for a run outside it. dir defaults
# to the app's cache directory, where it finds the bank without
# SONGBOOKS_SOUNDFONT.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
arg() { sed -n "s/^ARG $1=//p" "$here/Dockerfile" | head -n 1; }
commit=$(arg GUGS_COMMIT)
sf2_sha=$(arg GUGS_SF2_SHA256)
license_sha=$(arg GUGS_LICENSE_SHA256)
[ -n "$commit" ] && [ -n "$sf2_sha" ] && [ -n "$license_sha" ] || { echo "fetch-soundfont: no GUGS_* pins in $here/Dockerfile" >&2; exit 1; }

if [ $# -ge 1 ]; then
  dir=$1
elif [ "$(uname)" = Darwin ]; then
  dir="$HOME/Library/Caches/songbooks"
else
  dir="${XDG_CACHE_HOME:-$HOME/.cache}/songbooks"
fi
mkdir -p "$dir"

sha256() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1; }

# Into a new directory of its own first; moved in place once checked.
tmp=$(mktemp -d "$dir/.fetch-XXXXXX")
trap 'rm -rf "$tmp"' EXIT
base="https://raw.githubusercontent.com/mrbumpy409/GeneralUser-GS/$commit"
curl -fsSL -o "$tmp/GeneralUser-GS.sf2" "$base/GeneralUser-GS.sf2"
curl -fsSL -o "$tmp/GeneralUser-GS-LICENSE.txt" "$base/documentation/LICENSE.txt"
[ "$(sha256 "$tmp/GeneralUser-GS.sf2")" = "$sf2_sha" ] || { echo "fetch-soundfont: GeneralUser-GS.sf2 doesn't match its SHA-256" >&2; exit 1; }
[ "$(sha256 "$tmp/GeneralUser-GS-LICENSE.txt")" = "$license_sha" ] || { echo "fetch-soundfont: the license doesn't match its SHA-256" >&2; exit 1; }
mv "$tmp/GeneralUser-GS.sf2" "$tmp/GeneralUser-GS-LICENSE.txt" "$dir/"
echo "GeneralUser GS in $dir/GeneralUser-GS.sf2"
