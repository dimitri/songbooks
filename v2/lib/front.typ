// Front matter for an assembled songbook: title plate, album plates, and a
// contents table generated from the songs' own metadata.
//
// The sheets themselves are NOT re-rendered here. Each one is already built
// and verified as exactly one page, so the book is this front matter followed
// by a concatenation of those PDFs -- which is both far faster and guarantees
// the book shows precisely the pages that passed the one-page check.

#import "theme.typ": TARGETS, LIGHT, DARK, FONTS
#import "song.typ": parse-song

#let plate(body, fill: rgb("#0f0f0f"), t: none) = page(
  width: t.w, height: t.h, margin: t.margin, fill: fill, {
    set text(fill: white, font: FONTS.title)
    body
  })

#let title-plate(title, subtitle: none, logo: none, t: none) = plate(t: t, {
  v(1fr)
  align(center, {
    text(size: 42pt, weight: "bold", tracking: 0.08em, smallcaps(title))
    if subtitle != none {
      linebreak(); v(0.4em)
      text(size: 18pt, fill: rgb("#9a9a9a"), tracking: 0.12em, smallcaps(subtitle))
    }
  })
  v(1.2fr)
  if logo != none { align(center, image(logo, height: 42%)) }
  v(1fr)
})

#let plates(items, heading: none, t: none, fill: rgb("#0a1616")) = plate(
  t: t, fill: fill, {
    v(1fr)
    if heading != none {
      align(center, text(size: 28pt, tracking: 0.16em, smallcaps(heading)))
      v(2.2em)
    }
    stack(spacing: 1.6em, ..items.map(it => align(center, {
      image(it.img, height: 23%)
      v(0.7em)
      text(size: 13pt, fill: rgb("#c4c4c4"), tracking: 0.1em, smallcaps(it.label))
    })))
    v(1.15fr)
  })

// Contents generated from the song files, not hand-typed. The old books
// re-typed title/key/tempo into every titlepage and the copies had already
// drifted apart.
#let contents(songs, t: none, dark: false) = {
  let theme = if dark { DARK } else { LIGHT }
  page(width: t.w, height: t.h, margin: t.margin, fill: theme.bg, {
    set text(fill: theme.ink, font: FONTS.lyric)
    text(font: FONTS.title, size: 24pt, weight: "bold", "Contents")
    v(0.3em)
    line(length: 100%, stroke: 1pt + theme.rule)
    v(0.8em)
    set text(size: 11pt)
    table(
      columns: (auto, 1fr, auto, auto), stroke: none,
      align: (right, left, left, right), inset: (x: 0.4em, y: 0.42em),
      fill: (_, y) => if calc.odd(y) { theme.chip.lighten(55%) } else { none },
      table.header(
        text(weight: "bold", size: 9pt, "#"),
        text(weight: "bold", size: 9pt, "TITLE"),
        text(weight: "bold", size: 9pt, "KEY"),
        text(weight: "bold", size: 9pt, "BPM")),
      ..songs.enumerate().map(((i, s)) => (
        text(fill: theme.meta, str(i + 1)),
        text(weight: "medium", s.title),
        text(fill: theme.meta, s.at("key", default: "")),
        text(fill: theme.meta, s.at("tempo", default: "")),
      )).flatten())
  })
}

#let front(order-file, songdir, target: "a4", dark: false, plates-before: ()) = {
  let t = TARGETS.at(target)
  let slugs = read(order-file).split("\n").map(l => l.trim()).filter(l => l != "")
  let songs = slugs.map(sl => parse-song(songdir + "/" + sl + ".song"))
  for f in plates-before { f(t) }
  contents(songs, t: t, dark: dark)
}
