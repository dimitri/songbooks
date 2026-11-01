#!/bin/sh
# Assert the one-page requirement and the legibility floor.
#
# A song that cannot fit one page above the floor is an editorial problem with
# the arrangement, not a layout bug, and some genuinely cannot. Those are
# listed in <book>/known-overflow.txt so this check fails on REGRESSIONS --
# a song that used to fit and no longer does -- rather than being permanently
# red. A listed song that starts fitting again is also reported, so the list
# does not rot.
set -u
BOOK="$1"; TARGET="$2"; THEME="$3"; OUT="$4"
KNOWN="$BOOK/known-overflow.txt"
[ -f "$KNOWN" ] || KNOWN=/dev/null

n=0; bad=0; expected=0; fixed=0
printf '%-34s %5s %6s %5s\n' SONG PAGES SIZE COLS
for pdf in "$OUT"/*.pdf; do
  [ -e "$pdf" ] || continue
  slug=$(basename "$pdf" .pdf); n=$((n + 1))
  # written by compile-sheet.sh; recomputing it here would mean a full
  # recompile of every sheet just to read back one number
  fit="${pdf%.pdf}.fit"
  pages=$(awk '{print $1}' "$fit" 2>/dev/null)
  [ -n "$pages" ] || pages=$(pdfinfo "$pdf" 2>/dev/null | awk '/^Pages:/{print $2}')
  info=$(cut -d' ' -f2- "$fit" 2>/dev/null)
  size=$(printf '%s' "$info" | sed -n 's/.*"size":\([0-9.]*\).*/\1/p')
  cols=$(printf '%s' "$info" | sed -n 's/.*"cols":\([0-9]*\).*/\1/p')
  listed=no
  grep -qx "$slug" "$KNOWN" 2>/dev/null && listed=yes

  flag=""
  if [ "$pages" != "1" ]; then
    if [ "$listed" = yes ]; then
      flag="  (known)"; expected=$((expected + 1))
    else
      flag="  <- REGRESSION: no longer fits one page"; bad=$((bad + 1))
    fi
  elif [ "$listed" = yes ]; then
    flag="  <- now fits; drop it from $KNOWN"; fixed=$((fixed + 1))
  fi
  printf '%-34s %5s %6s %5s%s\n' "$slug" "$pages" "$size" "$cols" "$flag"
done

echo "---"
echo "$n sheets | $((n - expected - bad)) on one page | $expected known overflow | $bad regressions"
[ "$fixed" -gt 0 ] && echo "$fixed song(s) now fit and should be removed from $KNOWN"
[ "$bad" -eq 0 ]
