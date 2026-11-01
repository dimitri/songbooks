// Chord symbol: parsing, transposition, rendering.
//
// Typographic rules follow Brandt & Roemer, "Standardized Chord Symbol
// Notation" (1976) -- the actual ancestor of Real Book house style:
//   * capital letters for roots
//   * quality text smaller than the root (we use ~0.62em, re-weighted, not
//     naively scaled -- see Scoring Notes on superscript weight)
//   * accidental precedes the numeral (b5, not 5b)
//   * alterations in parentheses
//   * banned: minus sign, triangle, theta, phi, crossed-7, AUG
// Real U+266D / U+266F glyphs, never ASCII b / #.
//
// One deliberate deviation from Brandt & Roemer: they ban the triangle for a
// major seventh in favour of "MA7". A delta/triangle is overwhelmingly what
// real jazz lead sheets and Real Books actually print for it, so that's what
// we draw -- see MAJ below.

#let SHARP-NAMES = ("C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B")
#let FLAT-NAMES  = ("C", "Db", "D", "Eb", "E", "F", "Gb", "G", "Ab", "A", "Bb", "B")

// What a chord chart actually writes when nothing forces a choice. Spelling
// every black key as a sharp gives D#m and G# where every real chart writes
// Ebm and Ab; spelling them all flat gives Gb where F# is normal. This table
// is the conventional compromise and is what chord-chart software uses.
#let USUAL-NAMES = ("C", "Db", "D", "Eb", "E", "F", "F#", "G", "Ab", "A", "Bb", "B")

#let PITCH = (
  C: 0, "C#": 1, Db: 1, D: 2, "D#": 3, Eb: 3, E: 4, Fb: 4, "E#": 5,
  F: 5, "F#": 6, Gb: 6, G: 7, "G#": 8, Ab: 8, A: 9, "A#": 10, Bb: 10,
  B: 11, Cb: 11, "B#": 0,
)

// Keys conventionally spelled with flats.
#let FLAT-KEYS = ("F", "Bb", "Eb", "Ab", "Db", "Gb", "Dm", "Gm", "Cm", "Fm", "Bbm", "Ebm")

#let split-root(s) = {
  if s.len() == 0 { return ("", "") }
  let n = if s.len() > 1 and (s.at(1) == "b" or s.at(1) == "#") { 2 } else { 1 }
  (s.slice(0, n), s.slice(n))
}

// "Bb7/D" -> (root: "Bb", quality: "7", bass: "D")
#let parse(sym) = {
  let s = sym.trim()
  if s == "" or s == "N.C." or s == "%" { return (root: none, quality: s, bass: none) }
  let parts = s.split("/")
  let (root, quality) = split-root(parts.at(0))
  let bass = if parts.len() > 1 { split-root(parts.at(1)).at(0) } else { none }
  (root: root, quality: quality, bass: bass)
}

#let transpose-note(note, semitones, prefer-flat: auto) = {
  if note == none or note == "" { return note }
  let p = PITCH.at(note, default: none)
  if p == none { return note }
  let i = calc.rem(p + semitones + 120, 12)
  if prefer-flat == true { FLAT-NAMES.at(i) }
  else if prefer-flat == false { SHARP-NAMES.at(i) }
  else { USUAL-NAMES.at(i) }
}

#let transpose(sym, semitones, prefer-flat: auto) = {
  if semitones == 0 { return sym }
  let c = parse(sym)
  if c.root == none { return sym }
  let r = transpose-note(c.root, semitones, prefer-flat: prefer-flat)
  let b = transpose-note(c.bass, semitones, prefer-flat: prefer-flat)
  r + c.quality + if b != none { "/" + b } else { "" }
}

#let glyphs(s) = s.replace("b", sym.flat).replace("#", sym.sharp)

// Render one chord symbol. Three type tiers: root/bass at base size,
// quality raised and reduced, parentheses kept at base height.
#let CHORD-RATIO = 1.15

// The minor marker hangs BELOW the baseline; numeric extensions ride above it.
// Brandt & Roemer set quality text at half size on the baseline, but splitting
// the two makes "Gm7" readable at a glance: the quality and the extension stop
// competing for the same spot.
#let MINOR = regex("^(m|min|mi|-)")

// "maj" shares its leading "m" with minor ("m", "min", "mi"), and Typst's
// regex engine (Rust's `regex` crate) has no lookahead to disambiguate them in
// one pattern -- "Bbmaj7" was being parsed as root Bb + minor "m" (subscript)
// + extension "aj7" (superscript), which is wrong twice over. "maj" is tested
// explicitly and FIRST, and rendered as a delta/triangle superscript rather
// than the literal letters -- the standard jazz lead-sheet convention for a
// major seventh.
#let MAJ = regex("^maj")
#let TRIANGLE = "\u{25B3}"

#let show-chord(sym, fill: none) = {
  let c = parse(sym)
  let col = if fill == none { auto } else { fill }
  let body = if c.root == none {
    text(style: "italic", weight: "regular", c.quality)
  } else {
    text(weight: "bold")[#glyphs(c.root)]
    let q = c.quality
    let maj = q.match(MAJ)
    let m = if maj == none { q.match(MINOR) } else { none }
    if maj != none {
      h(0.02em)
      text(size: 0.62em, weight: "bold", baseline: -0.34em)[#TRIANGLE]
      q = q.slice(maj.end)
      if q != "" { h(0.02em) }
    } else if m != none {
      // Minor sits BELOW the baseline, but "below the baseline" is not the
      // same as "centred against the root": a flat 0.22em drop reads as
      // sitting too low, hanging off the bottom of the root glyph rather than
      // nested against it. 0.14em centres the small glyph's own vertical
      // middle against the root's lower half instead.
      h(0.02em)
      text(size: 0.6em, weight: "bold", baseline: 0.14em)[#glyphs(m.text)]
      q = q.slice(m.end)
      if q != "" { h(0.03em) }
    }
    if q != "" {
      text(size: 0.62em, weight: "bold", baseline: -0.34em)[#glyphs(q)]
    }
    if c.bass != none {
      h(0.04em)
      text(weight: "regular", size: 0.9em)[/#glyphs(c.bass)]
    }
  }
  let sized = text(size: CHORD-RATIO * 1em, body)
  if fill == none { sized } else { text(fill: col, sized) }
}
