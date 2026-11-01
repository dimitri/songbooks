// Plain lyric text under each section -- no inline chords.
//
// An earlier version placed chords over the syllable they fall on, using
// `stack(dir: ltr, ...)` of fixed-width boxes. That cannot wrap: a lyric line
// long enough to need two physical lines simply overflowed the column's right
// edge and rendered on top of whatever was next to it. The harmony already
// lives in the grid at the top of each section, so the fix is also the
// simplification: join each line's segments back into one string of real text
// and let it wrap like any other paragraph.

#let lyric-line(segments) = segments.map(s => s.text).join()

#let lyric-block(lines, theme: none) = {
  set par(leading: 0.42em, justify: false)
  stack(spacing: 0.3em, ..lines.map(l => lyric-line(l)))
}
