// Bar grid: the Real Book / grille d'accords idiom.
//
// 4 measures per line, always, unless a section declares otherwise.
// That is the single highest-leverage layout decision: a 32-bar AABA form
// becomes 8 lines with each 8-bar section occupying exactly 2 lines, and a
// 12-bar blues becomes 3 lines. Confirmed as near-universal across iReal Pro
// (16 cells / 4), ChordPro's shape="1+4x4+1" default, Chord Sheet Maker's
// default, and Nashville practice.
//
// Barlines are decorations on cell edges, not content (iReal Pro's model), and
// form apparatus -- section letters, repeats, voltas -- lives in the margins
// and on boundaries, consuming no cells.

#import "chord.typ": show-chord
#import "staff.typ": staff-system

#let BARS-PER-LINE = 4

// A measure is a dict: (chords: ("Gm",), open: bool, close: bool, volta: none|str)
#let measure(chords, open: false, close: false, volta: none) = (
  chords: chords, open: open, close: close, volta: volta,
)

#let make-cell(m, theme, beats: 4) = {
  let n = m.chords.len()
  let body = if n == 0 {
    // empty measure: beat slashes, so the bar still reads as time passing
    text(fill: theme.faint, size: 0.85em,
      grid(columns: beats * (1fr,), ..range(beats).map(_ => align(center)[/])))
  } else if n == 1 {
    align(left, show-chord(m.chords.at(0), fill: theme.chord))
  } else {
    // split bar: cells share the measure as fr units (the French quartered
    // cell, read left to right), which is more compact than Nashville
    // underlining for 3-4 chords per bar
    grid(columns: n * (1fr,), column-gutter: 0.2em,
      ..m.chords.map(c => show-chord(c, fill: theme.chord)))
  }
  box(width: 100%, inset: (x: 0.45em, y: 0.42em), body)
}

#let bargrid(measures, theme: none, per-line: BARS-PER-LINE, time: none,
             tempo: none) = context {
  if measures.len() == 0 { return none }
  // The staff is sized from the surrounding type so the chord symbols on it
  // stay in proportion with the lyrics, and so the whole system shrinks with
  // the fit loop.
  let sp = 0.42 * text.size

  // per-line is usually a uniform bar count; an array instead gives each
  // row's size explicitly, for a form whose last row is not the same length
  // as the rest (see song.typ's "bars:" parsing).
  let is-variable = type(per-line) == array

  layout(avail => {
    // MuseScore's minMeasureWidth is 8 S, but a bar drawn with a single
    // whole-bar slash needs far less than one carrying four separate ones.
    // Sized from the widest row, so no row ends up wider than the page.
    let fit-per-line = if is-variable { calc.max(..per-line) } else { per-line }
    // If the grouping will not fit at this staff size, shrink the staff rather
    // than the grouping.
    let sp = calc.min(sp, avail.width / (fit-per-line * 5.5))
    stack(spacing: 0.6 * sp,
    ..{
      let rows = ()
      let i = 0
      if is-variable {
        let sizes = per-line
        let k = 0
        while i < measures.len() {
          let n = sizes.at(calc.min(k, sizes.len() - 1))
          rows.push(measures.slice(i, calc.min(i + n, measures.len())))
          i += n
          k += 1
        }
      } else {
        while i < measures.len() {
          rows.push(measures.slice(i, calc.min(i + per-line, measures.len())))
          i += per-line
        }
      }
      rows.enumerate().map(((r, row)) =>
        staff-system(row, sp, avail.width, theme,
                     // signature and tempo on the section's first system only
                     time: if r == 0 { time } else { none },
                     tempo: if r == 0 { tempo } else { none }))
    })
  })
}
