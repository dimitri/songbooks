#!/bin/sh
# Render the few fragments that carry real notation (TabStaff / staff) to SVG,
# once, offline. Everything else is a chord grid and is drawn natively, so
# LilyPond is no longer part of the document build.
#
# -dcrop trims to the ink; the SVG backend keeps it vector, which stays crisp
# and avoids the PDF-version and PDF/A constraints that embedded PDFs carry.
# NOTE: with the svg backend, -dcrop still tries to emit a PNG crop and exits
# non-zero when it cannot -- the SVG is written regardless, so we test for the
# file rather than trusting the exit status.
set -u
SRCBOOK="${1:-../Mercy}"
OUT="${2:-Mercy/scores}"
mkdir -p "$OUT"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
n=0
for ly in "$SRCBOOK"/*/*.ly "$SRCBOOK"/*/*/*.ly; do
  [ -e "$ly" ] || continue
  grep -q 'TabStaff\|\\relative\|\\clef' "$ly" || continue
  song=$(basename "$(dirname "$ly")")
  name=$(basename "$ly" .ly)
  # extract-score.py keeps only the notation blocks and gives them a paper
  # block of their own: the repo's header.ly pins line-width to 6cm, which is
  # what wrapped these riffs into unreadable narrow systems.
  if python3 extract-score.py "$ly" "$OUT/$song-$name.svg" 170; then
    echo "  $OUT/$song-$name.svg"
    n=$((n+1))
  else
    echo "warn: no notation extracted from $ly" >&2
  fi
done
echo "$n score fragments rendered"
