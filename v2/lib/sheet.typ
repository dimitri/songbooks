// The one-page sheet template.
//
// The headline requirement is that every song occupies exactly one page on
// every target. That is achieved by measuring the assembled body and bisecting
// the type size until it fits -- which is the one thing neither LaTeX+songs nor
// ChordPro can do automatically.
//
// Hybrid structure, which resolves the grid-vs-lyrics tradeoff rather than
// picking a side: the bar grid is the authoritative timing document (compact,
// repeat-collapsed, beat-accurate) and the chorded lyrics are the vocal
// document (singable). Neither duplicates the other's work.

#import "theme.typ": TARGETS, LIGHT, DARK, FONTS, SCALE
#import "grid.typ": bargrid
#import "lyrics.typ": lyric-block
#import "chord.typ": glyphs

// `repeat-of-prior` marks a section that is declared "(repeat)" in the source
// and carries no grid or lyrics of its own -- i.e. nothing follows the chip at
// all. Rendered bare, that read as a stray empty label; a reader's first
// guess was that content had gone missing, not that the form repeats. A
// small "· repeat" tag on the chip itself removes the ambiguity without
// inventing a new bar symbol for something that isn't a bar.
#let section-chip(name, theme, repeat-of-prior: false) = block(
  above: 0.62em, below: 0.3em,
  box(fill: theme.chip, inset: (x: 0.5em, y: 0.22em), radius: 2pt,
      text(size: SCALE.chip, weight: "bold", tracking: 0.07em,
           fill: theme.chip-ink, {
             upper(name)
             if repeat-of-prior {
               text(weight: "regular", style: "italic", tracking: 0.02em,
                    "  ·  repeat")
             }
           })))

#let header(song, theme) = {
  block(below: 0.7em, {
    grid(columns: (1fr, auto), align: (left + bottom, right + bottom),
      text(font: FONTS.title, size: SCALE.title, weight: "bold",
           fill: theme.ink, song.title),
      text(size: SCALE.meta, fill: theme.meta, {
        let bits = ()
        if song.at("artist", default: none) != none { bits.push(song.artist) }
        if song.at("key", default: none) != none { bits.push(glyphs(song.key)) }
        if song.at("tempo", default: none) != none { bits.push(song.tempo + " bpm") }
        if song.at("time", default: none) != none { bits.push(song.time) }
        if song.at("capo", default: none) != none { bits.push("capo " + song.capo) }
        bits.join("  ·  ")
      }))
    v(-0.35em)
    line(length: 100%, stroke: 1.1pt + theme.rule)
  })
}

#let one-section(s, theme) = {
  section-chip(s.label, theme,
    repeat-of-prior: s.at("repeat", default: false)
                     and s.measures.len() == 0 and s.lines.len() == 0)
  if s.at("score", default: none) != none {
    block(below: if s.lines.len() > 0 { 0.45em } else { 0.1em },
          box(width: 100%, image("/" + s.score-path, width: 100%)))
  }
  if s.measures.len() > 0 {
    block(below: if s.lines.len() > 0 { 0.45em } else { 0.1em },
          bargrid(s.measures, theme: theme, per-line: s.per-line,
                    time: s.at("time", default: none),
                    tempo: s.at("tempo-mark", default: none)))
  }
  if s.lines.len() > 0 { lyric-block(s.lines, theme: theme) }
}

#let body-of(song, theme, cols: 1, bars: none) = {
  // The tempo mark goes on the first system that actually carries a grid.
  let first-grid = song.sections.position(s => s.measures.len() > 0)
  // A section is never split across a column break: the chip, its grid and its
  // lyrics are one unbreakable unit.
  let secs = song.sections.enumerate().map(((i, s)) => {
    let s = if bars == none { s } else { (..s, per-line: bars) }
    let s = if i == first-grid {
      (..s,
       tempo-mark: song.at("tempo", default: none),
       time: if s.at("time", default: none) != none { s.time }
             else { song.at("time", default: none) })
    } else { s }
    block(breakable: false, one-section(s, theme))
  })
  if cols == 1 { secs.join() } else { columns(cols, gutter: 20pt, secs.join()) }
}

// Bisect the lyric size until the whole body fits the text region. Bisection
// is monotone, so it converges cleanly; 14 halvings over the range resolves to
// well under a tenth of a point.
#let fit-one-page(song, theme, k, ceiling: none, fit: none) = layout(avail => context {
  // Measure the complete assembly -- header and body together -- so that the
  // spacing *between* blocks is accounted for. Measuring them separately
  // under-counts by about one line and the sheet spills onto a second page.
  let page-at(sz, cols, bars) = {
    set text(size: sz, font: FONTS.lyric, fill: theme.ink)
    set par(leading: sz * 0.34, justify: false)
    header(song, theme)
    body-of(song, theme, cols: cols, bars: bars)
  }
  // measure() cannot see column balancing: given unbounded height it pours
  // everything into the first column and reports the single-column height. So
  // for two columns we measure the body as one flow at the COLUMN width and
  // divide, with slack for the imperfect balancing that unbreakable sections
  // cause.
  let GUTTER = 20pt
  let BALANCE-SLACK = 1.14
  let head-h = measure(block(width: avail.width, header(song, theme))).height
  let fits(sz, cols, bars) = {
    if cols == 1 {
      measure(block(width: avail.width, page-at(sz, 1, bars)), width: avail.width)
        .height <= avail.height
    } else {
      let cw = (avail.width - GUTTER) / cols
      let bh = measure(
        block(width: cw, {
          set text(size: sz, font: FONTS.lyric, fill: theme.ink)
          set par(leading: sz * 0.34, justify: false)
          body-of(song, theme, cols: 1, bars: bars)
        }), width: cw,
      ).height
      bh / cols * BALANCE-SLACK <= avail.height - head-h
    }
  }
  // Largest size that fits in a given column count. Bisection is monotone, so
  // it converges cleanly; 14 halvings resolve the range to well under a tenth
  // of a point.
  let best(cols, bars) = {
    let lo = SCALE.floor * k
    // A ceiling the build can lower on retry: estimating column balance is
    // inherently approximate, so the build verifies the real page count and
    // lowers this until the sheet genuinely fits.
    let hi = if fit != none { fit.size * 1pt }
             else if ceiling == none { SCALE.lyric-max * k } else { ceiling }
    if lo > hi { return 0pt }
    if fits(hi, cols, bars) { return hi }
    if not fits(lo, cols, bars) { return 0pt }
    for _ in range(14) {
      let mid = (lo + hi) / 2
      if fits(mid, cols, bars) { lo = mid } else { hi = mid }
    }
    lo
  }
  // The fallback ladder, in order of how much they cost the reader. Four bars
  // per system is the convention and the ONLY bar count this ladder will pick
  // for a blues/standard form -- an earlier version added an 8-bars-per-line
  // rung that bought a little type size on a couple of songs, but it also
  // fired on classic 8- and 12-bar forms that are supposed to read as two or
  // three rows of four, which is a far more common and more important case.
  // Two columns, unconditionally. A corpus-wide check found 1-column never
  // actually wins: every song that fits at all fits better at 2 columns, and
  // 1-column only ever showed up as the failure fallback for songs that don't
  // fit at either -- a lyric line rarely uses the full page width, so the
  // single-column option just wastes the right half of the page without
  // buying anything. Still bisect bar count the same way.
  let cols = 2
  let bars = none
  let chosen = best(cols, bars)
  let failed = chosen == 0pt
  if failed { chosen = SCALE.floor * k }
  // Expose the fitted size so the build can assert that nothing silently went
  // below the legibility floor. A song that cannot reach the floor on one page
  // is an editorial signal, not a layout problem.
  [#metadata((
    song: song.title,
    size: calc.round(chosen.pt(), digits: 2),
    floor: calc.round((SCALE.floor * k).pt(), digits: 2),
    cols: cols,
    bars: if bars == none { 4 } else { bars },
    fits: not failed,
  ))<fit>]
  page-at(chosen, cols, bars)
})

// The fitted body of one sheet, for use inside a larger document. The fit
// measures the region it is given, so a song placed at the top of a fresh page
// gets the whole page to work with.
#let sheet-body(song, target: "a4", dark: false, ceiling: none, fit: none) = {
  let t = TARGETS.at(target)
  let theme = if dark { DARK } else { LIGHT }
  fit-one-page(song, theme, t.k, ceiling: ceiling, fit: fit)
}

// A standalone one-song PDF.
#let sheet(song, target: "a4", dark: false, ceiling: none) = {
  let t = TARGETS.at(target)
  let theme = if dark { DARK } else { LIGHT }
  set document(title: song.title, author: song.at("artist", default: ""))
  set page(width: t.w, height: t.h, margin: t.margin, fill: theme.bg)
  set text(font: FONTS.lyric, fill: theme.ink, lang: "en", hyphenate: false)
  fit-one-page(song, theme, t.k, ceiling: ceiling)
}
