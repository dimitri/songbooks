// A songbook: front matter, a linked table of contents, then one page per song.
//
// This is a SINGLE Typst document rather than a concatenation, because a PDF
// link needs its destination in the same file -- pdfunite cannot rewrite link
// destinations across the files it joins.
//
// It is still fast, because it does not re-derive each song's layout: the
// per-song build already found the largest size that fits one page and wrote it
// to a .fit sidecar, and `fits.json` hands those back here. Searching again
// inside one 70-page document is what made the first version take minutes.

#import "theme.typ": TARGETS, LIGHT, DARK, FONTS, SCALE
#import "song.typ": parse-song
#import "sheet.typ": sheet-body
#import "chord.typ": glyphs

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

// One page: the album's own cover introduces it AND lists its songs, both on
// the same background -- a separate light-background contents page after a
// dark cover plate read as two unrelated pages rather than one album entry.
// Sized to fit a 10-song album (the largest here) with real margin to spare;
// the image height is the dial to turn if a future album needs more rows.
#let album-page(title, img, entries, year: none, t: none,
                fill: rgb("#0a1616")) = page(
    width: t.w, height: t.h, margin: t.margin, fill: fill, {
  set text(font: FONTS.lyric, fill: white)
  align(center, image(img, height: 30%))
  v(0.9em)
  align(center, {
    text(font: FONTS.title, size: 20pt, tracking: 0.1em, smallcaps(title))
    if year != none {
      linebreak(); v(0.15em)
      text(size: 10pt, fill: rgb("#9a9a9a"), tracking: 0.14em, str(year))
    }
  })
  v(1.1em)
  line(length: 100%, stroke: 0.6pt + rgb("#3a3a3a"))
  v(0.3em)
  set text(size: 9pt)
  for (i, e) in entries.enumerate() {
    let row = grid(columns: (1.6em, 1fr, 3em, 2.8em, 2.2em), column-gutter: 0.4em,
      align: (right + horizon, left + horizon, left + horizon,
              right + horizon, right + horizon),
      inset: (y: 0.3em),
      text(fill: rgb("#8a8a8a"), size: 8pt, str(i + 1)),
      text(weight: "medium", e.title),
      text(fill: rgb("#e8a33d"), weight: "medium", size: 8.5pt, glyphs(e.key)),
      text(fill: rgb("#9a9a9a"), size: 8.5pt, e.tempo),
      context text(fill: rgb("#9a9a9a"), size: 8.5pt, weight: "medium",
                   str(counter(page).at(e.label).first())))
    // the whole row is the link target, not just the title -- same rule as
    // the flat contents() table below
    link(e.label, box(width: 100%, row))
    line(length: 100%, stroke: 0.35pt + rgb("#2a2a2a"))
  }
})

// Kept for callers that still want a cover-only plate with no listing (the
// combined album-page above is what the per-album songbook sections use).
#let album-divider(title, img, year: none, t: none,
                    fill: rgb("#0a1616")) = plate(t: t, fill: fill, {
  v(1fr)
  align(center, image(img, height: 46%))
  v(1.1em)
  align(center, {
    text(size: 24pt, tracking: 0.1em, smallcaps(title))
    if year != none {
      linebreak(); v(0.2em)
      text(size: 12pt, fill: rgb("#9a9a9a"), tracking: 0.14em, str(year))
    }
  })
  v(1.3fr)
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

// --- contents --------------------------------------------------------------
// Generated from the songs' own metadata, never hand-typed: the old books
// re-typed title/key/tempo into every titlepage and the copies had drifted.
// Every row links to its page.
//
// One row of a contents table.
#let contents-row(i, e, th, accent) = {
  let row = grid(
    columns: (1.6em, 1fr, 3em, 2.8em, 2.2em), column-gutter: 0.4em,
    align: (right + horizon, left + horizon, left + horizon,
            right + horizon, right + horizon),
    inset: (y: 0.22em),
    text(fill: th.meta, size: 8pt, str(i + 1)),
    {
      text(weight: "medium", size: 9.5pt, e.title)
      if e.artist != "" {
        text(size: 7.5pt, fill: th.meta, "   " + e.artist)
      }
    },
    text(fill: accent, weight: "medium", size: 8.5pt, glyphs(e.key)),
    text(fill: th.meta, size: 8.5pt, e.tempo),
    context text(fill: th.meta, size: 8.5pt, weight: "medium",
                 str(counter(page).at(e.label).first())),
  )
  // the whole row is the link target, not just the title
  block(below: 0pt, {
    link(e.label, box(width: 100%, row))
    line(length: 100%, stroke: 0.4pt + th.rule.lighten(55%))
  })
}

#let contents-head(th) = grid(columns: (1.6em, 1fr, 3em, 2.8em, 2.2em),
  column-gutter: 0.4em,
  ..("", "TITLE", "KEY", "TEMPO", "PAGE").map(c =>
    text(size: 7pt, weight: "bold", tracking: 0.1em, fill: th.meta, c)))

// `cols`: how many columns to lay the rows out in. A flat book with "# SET:"
// groups (Mercy) is short enough, even with every song in it, to fit several
// sets side by side on one page -- which also sidesteps the alternative of
// paginating by a fixed row count, which cut a set's rows at an arbitrary
// point and reprinted its own header on the next page, indistinguishable at
// a glance from a genuinely new group starting there.
#let contents(entries, t: none, dark: false, title: "Contents", cols: 1) = {
  let th = if dark { DARK } else { LIGHT }
  let accent = th.chord
  set page(width: t.w, height: t.h, margin: t.margin, fill: th.bg)
  set text(fill: th.ink, font: FONTS.lyric)

  block(below: 0.6em, {
    text(font: FONTS.title, size: 20pt, weight: "bold", tracking: 0.02em, title)
    h(1fr)
    text(size: 8.5pt, fill: th.meta, tracking: 0.14em,
         upper(str(entries.len()) + " songs"))
    v(-0.3em)
    line(length: 100%, stroke: 1.2pt + th.rule)
  })

  set text(size: 9.5pt)

  let group-header(name) = block(above: 0.3em, below: 0.3em, breakable: false,
    text(size: 8.5pt, weight: "bold", tracking: 0.1em, fill: accent, upper(name)))

  let body = {
    contents-head(th)
    v(0.2em)
    line(length: 100%, stroke: 0.5pt + th.rule.lighten(55%))
    for (i, e) in entries.enumerate() {
      if e.group != none and (i == 0 or entries.at(i - 1).group != e.group) {
        group-header(e.group)
      }
      contents-row(i, e, th, accent)
    }
  }
  if cols > 1 { columns(cols, gutter: 28pt, body) } else { body }
}

#let book(order-file, songdir, target: "a4", dark: false, front: (),
          fits: (:), title: "Contents", dividers: (:)) = {
  let t = TARGETS.at(target)
  let theme = if dark { DARK } else { LIGHT }
  // A line "# --- Title (Year) ---" introduces the songs that follow, until
  // the next marker or the end of the file. `dividers` maps that exact Title
  // to the artwork to show; a marker with no matching entry is just a comment.
  let raw = read(order-file).split("\n").map(l => l.trim())
  let slugs = ()
  let divider-before = (:)   // slug -> (title, year): a full body-page divider
  let set-of = (:)           // slug -> set name: a TOC-only grouping, no extra page
  let pending = none
  let cur-set = none
  for l in raw {
    if l == "" { continue }
    if l.starts-with("#") {
      // "# SET: Name" groups the contents table only -- no page is inserted
      // in the body for it, unlike an album/front-matter divider.
      let sm = l.match(regex("^#\s*SET:\s*(.+)$"))
      if sm != none { cur-set = sm.captures.at(0); continue }
      let m = l.match(regex("^#\s*-+\s*(.+?)\s*(?:\((\d{4})\))?\s*-+$"))
      if m != none { pending = (title: m.captures.at(0), year: m.captures.at(1)) }
      continue
    }
    if pending != none { divider-before.insert(l, pending); pending = none }
    if cur-set != none { set-of.insert(l, cur-set) }
    slugs.push(l)
  }
  let songs = slugs.map(sl => parse-song(songdir + "/" + sl + ".song"))

  for f in front { f(t) }

  let entries = songs.enumerate().map(((i, s)) => (
    title: s.title,
    artist: s.at("artist", default: ""),
    key: s.at("key", default: ""),
    tempo: s.at("tempo", default: ""),
    label: label("song-" + slugs.at(i)),
    group: set-of.at(slugs.at(i), default: none),
    divider: divider-before.at(slugs.at(i), default: none),
  ))

  let song-page(i, s) = page(width: t.w, height: t.h, margin: t.margin,
      fill: theme.bg, {
    set text(font: FONTS.lyric, fill: theme.ink, lang: "en", hyphenate: false)
    // anchor for the contents link
    place(top + left, dx: 0pt, dy: 0pt, [#metadata(none)#label("song-" + slugs.at(i))])
    sheet-body(s, target: target, dark: dark,
               fit: fits.at(slugs.at(i), default: none))
  })

  if dividers.len() > 0 {
    // Album-style books (Other Lies): the contents table is PER ALBUM, placed
    // right after that album's own divider plate, not one combined table for
    // the whole songbook up front -- a reader picking up the book for one
    // album's songs should not have to hunt through every other album's rows
    // to find them.
    let i = 0
    while i < entries.len() {
      let d = entries.at(i).divider
      // this group runs until the next divider (or the end)
      let j = i + 1
      while j < entries.len() and entries.at(j).divider == none { j += 1 }
      let group = entries.slice(i, j)
      if d != none and d.title in dividers {
        // the cover and the listing are ONE page, same background -- a
        // dark cover plate followed by a separate light contents page read
        // as two unrelated pages rather than one album entry.
        (dividers.at(d.title))(d.title, d.year, t, group)
      } else {
        contents(group, t: t, dark: dark, title: title)
      }
      for k in range(i, j) { song-page(k, songs.at(k)) }
      i = j
    }
  } else {
    // Flat books (Mercy, Bands Camp): one combined table, optionally broken
    // into SET sub-headers within it (see set-of above). Two columns rather
    // than paginating by a fixed row count -- a book this size fits on one
    // page that way, and a group never gets cut and re-headered mid-list.
    contents(entries, t: t, dark: dark, title: title, cols: 2)
    for (i, s) in songs.enumerate() { song-page(i, s) }
  }
}

// --- an original front plate ------------------------------------------------
// A circle-of-fifths wheel showing which keys the book actually lives in, with
// each segment weighted by how many songs sit there. Drawn from the songs' own
// metadata, so it is specific to this book rather than decoration -- and it is
// original artwork, not someone else's cover.
#let key-wheel(songs, t: none, heading: "The Keys", fill: rgb("#0f0f0f")) = {
  let FIFTHS = ("C", "G", "D", "A", "E", "B", "F#", "Db", "Ab", "Eb", "Bb", "F")
  let counts = (:)
  for s in songs {
    let k = s.at("key", default: "")
    if k == "" { continue }
    let root = if k.len() > 1 and (k.at(1) == "b" or k.at(1) == "#") {
      k.slice(0, 2) } else { k.slice(0, 1) }
    counts.insert(root, counts.at(root, default: 0) + 1)
  }
  let most = calc.max(1, ..FIFTHS.map(k => counts.at(k, default: 0)))

  // Geometry is plain arithmetic against a fixed canvas size, with no `layout`
  // or `context` involved -- those triggered introspection re-layout that
  // never converged once this plate sat inside the book alongside 70 other
  // pages' worth of fit lookups.
  let W = 420pt
  let r = W / 2
  let cx = r
  let cy = r
  let nodes = FIFTHS.enumerate().map(((i, k)) => {
    let a = 90deg - i * 30deg
    let n = counts.at(k, default: 0)
    let rr = r * (0.42 + 0.5 * n / most)
    (k: k, n: n, x: cx + rr * calc.cos(a), y: cy - rr * calc.sin(a),
     sx: cx + r * 0.3 * calc.cos(a), sy: cy - r * 0.3 * calc.sin(a))
  })

  plate(t: t, fill: fill, {
    v(1fr)
    align(center, text(size: 26pt, tracking: 0.16em, smallcaps(heading)))
    v(2em)
    align(center, box(width: W, height: W, {
      for nd in nodes {
        line(start: (nd.sx, nd.sy), end: (nd.x, nd.y),
          stroke: (if nd.n > 0 { 1.4pt } else { 0.4pt })
                + (if nd.n > 0 { rgb("#e8a33d") } else { rgb("#3a3a3a") }))
      }
      for nd in nodes {
        let d = if nd.n > 0 { (0.9 + 0.5 * nd.n / most) * r * 0.1 } else { r * 0.035 }
        place(top + left, dx: nd.x - d / 2, dy: nd.y - d / 2,
          circle(radius: d / 2,
                 fill: if nd.n > 0 { rgb("#e8a33d") } else { rgb("#3a3a3a") },
                 stroke: none))
        place(top + left, dx: nd.x - 2em, dy: nd.y + d / 2 + 0.35em,
          box(width: 4em, align(center,
            text(size: 10pt, fill: if nd.n > 0 { white } else { rgb("#6a6a6a") },
                 tracking: 0.08em,
                 nd.k + if nd.n > 0 { "  " + str(nd.n) } else { "" }))))
      }
    }))
    v(1.3fr)
  })
}
