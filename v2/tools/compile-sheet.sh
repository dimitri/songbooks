#!/bin/sh
# Compile one sheet and VERIFY the one-page invariant rather than trusting the
# column-balance estimate: typst's measure() cannot see column balancing, so
# the estimate is necessarily approximate. On overflow, lower the type ceiling
# and retry. We give up only at the legibility floor, which is an editorial
# signal about the song, not a layout failure to paper over.
#
# The chosen size/columns are written to a .fit sidecar so the check step can
# read them without recompiling every sheet.
set -u
SONG="$1"; OUT="$2"; TARGET="${3:-a4}"; THEME="${4:-light}"
# TYPST_FLAGS carries --root and --font-path from mk/config.mk. Without the
# latter, every sheet compile silently fell back past the vendored Lato to
# whatever sans-serif happens to be installed on the machine doing the build
# -- invisible locally (this Mac has PT Sans), but not reproducible on CI.
FLAGS="${TYPST_FLAGS:---root .}"
mkdir -p "$(dirname "$OUT")"
FIT="${OUT%.pdf}.fit"
ceiling=""
i=0
while [ "$i" -lt 8 ]; do
  # shellcheck disable=SC2086
  typst compile $FLAGS render.typ "$OUT" \
    --input song="/$SONG" --input target="$TARGET" --input theme="$THEME" \
    ${ceiling:+--input ceiling=$ceiling} \
    --deps "${OUT%.pdf}.d" --deps-format make --diagnostic-format short || exit 1
  pages=$(pdfinfo "$OUT" 2>/dev/null | awk '/^Pages:/{print $2}')
  # shellcheck disable=SC2086
  info=$(typst eval $FLAGS --in render.typ 'query(<fit>).first().value' \
         --input song="/$SONG" --input target="$TARGET" --input theme="$THEME" \
         ${ceiling:+--input ceiling=$ceiling} --format json 2>/dev/null)
  printf '%s %s\n' "$pages" "$info" > "$FIT"
  [ "$pages" = "1" ] && exit 0
  size=$(printf '%s' "$info" | sed -n 's/.*"size":\([0-9.]*\).*/\1/p')
  [ -z "$size" ] && break
  ceiling=$(awk -v s="$size" 'BEGIN{printf "%.2f", s*0.94}')
  i=$((i + 1))
done
echo "warn: $SONG still $pages pages at the floor -- song needs editing" >&2
exit 0
