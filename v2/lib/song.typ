// Parser for the .song source format -- the single source of truth per song.
//
// Metadata, chord grid and chorded lyrics live in one file, so a song has
// exactly one key and exactly one transposition. The old layout kept chords in
// two places (a LilyPond .ly grid and inline \[Chord] markers in the .tex) with
// two independent transposition mechanisms that had to be kept in agreement by
// hand.
//
//   title:  Banshee
//   artist: Kendra Morris
//   key:    Gm
//   tempo:  65
//   time:   4/4
//
//   [verse]
//   grid: |: Gm | Bb | Dm | C | Gm | Bb | F | C :|
//   Chord-marked [Gm]lyric line
//
//   [solo]
//   grid: | Gm | Bb | F | C |

#import "chord.typ": transpose, parse as parse-chord
#import "chord.typ": FLAT-KEYS

#let META-KEYS = ("title", "artist", "key", "tempo", "time", "capo",
                  "transpose", "map", "idiom", "flats", "reference",
                  "cover", "cover-credit")

// Sub-beat duration tag, e.g. "Gm:8" or "Gm:4." -- LilyPond's own
// denominator-and-dot syntax (`c4.`, `c8`, ...), separated from the token by
// a colon rather than appended bare the way LilyPond does it: a chord
// symbol's own quality routinely ends in a digit (Cm7, G9, Bm7b5...), so a
// bare trailing number would collide with real chord names instead of
// marking a duration. The internal unit is the SIXTEENTH note -- the
// coarsest grain that keeps every duration through a dotted eighth an exact
// integer; a dotted sixteenth (1.5) is not representable yet, matching the
// "probably 16ths later" scope this was built against.
#let DUR-TAG = regex("^(.*):([0-9]+)(\.?)$")
#let parse-dur(tok) = {
  let m = tok.match(DUR-TAG)
  if m == none { (bare: tok, dur: none) }
  else {
    let denom = int(m.captures.at(1))
    let base = 16 / denom
    (bare: m.captures.at(0),
     dur: if m.captures.at(2) == "." { base * 3 / 2 } else { base })
  }
}

// "|: Gm | Bb | Dm | C :|" -> array of measure dicts
#let parse-grid(spec, semitones, flat) = {
  let out = ()
  let s = spec.trim()
  let open-next = false
  let toks = s.split("|")
  let i = 0
  let pending-volta = none
  for raw in toks {
    let tok = raw.trim()
    if tok == "" { continue }
    // "|:" leaves a bare ":" leading the next token; ":|" leaves a trailing ":"
    let opens = tok.starts-with(":")
    let closes = tok.ends-with(":")
    if opens and tok.len() > 0 { tok = tok.slice(1).trim() }
    if closes and tok.len() > 0 { tok = tok.slice(0, -1).trim() }
    // per-bar meter change, e.g. "{2/4} G" -- parentheses are already taken
    // by volta markers
    let meter = none
    if tok.starts-with("{") and tok.contains("}") {
      let e = tok.position("}")
      meter = tok.slice(1, e)
      tok = tok.slice(e + 1).trim()
    }
    // volta marker, e.g. "(1.) Gm"
    let volta = none
    if tok.starts-with("(") and tok.contains(")") {
      let e = tok.position(")")
      volta = tok.slice(1, e)
      tok = tok.slice(e + 1).trim()
    }
    if tok == "" and not opens and not closes { continue }
    let raw-toks = if tok == "" { () } else { tok.split(" ").filter(c => c != "") }
    let chords = raw-toks.map(c => {
      let bare = parse-dur(c).bare
      if bare == "%" or bare == "/" or bare == "." or bare == "R" or bare == "r" { bare }
      else { transpose(bare, semitones, prefer-flat: flat) }
    })
    // Sub-beat events, built only when at least one token on this bar
    // carries a ":duration" tag -- the rhythm-slash renderer (stems, beams,
    // flags) reads this instead of `chords` for such a bar; every other bar
    // keeps rendering exactly as before (one slash per quarter-beat token),
    // so this is purely additive. "." extends the previous event by one
    // quarter (4 sixteenths), matching what "." already means untagged;
    // finer sustain is written as an explicit duration on the chord itself
    // (`Gm:4.`), not by chaining dots. Lowercase "r" is a sub-beat rest.
    let has-dur = raw-toks.any(t => t.contains(":"))
    let events = if not has-dur { () } else {
      let acc = ()
      for c in raw-toks {
        let (bare, dur) = (parse-dur(c).bare, parse-dur(c).dur)
        let d = if dur != none { dur } else { 4 }
        if bare == "." and acc.len() > 0 {
          let last = acc.last()
          acc.at(acc.len() - 1) = (chord: last.chord, dur: last.dur + d)
        } else if bare == "r" or bare == "R" {
          acc.push((chord: none, dur: d))
        } else {
          acc.push((chord: transpose(bare, semitones, prefer-flat: flat), dur: d))
        }
      }
      acc
    }
    out.push((chords: chords, events: events, open: opens or open-next,
              close: closes, volta: volta, meter: meter))
    open-next = false
  }
  out
}

// "Pla[Gm]ceholder words [Bb]here" -> array of (chord, text) segments
#let parse-lyric(line, semitones, flat) = {
  let segs = ()
  let rest = line
  let pending = none
  let buf = ""
  while rest.len() > 0 {
    let o = rest.position("[")
    if o == none { buf += rest; rest = "" ; break }
    buf += rest.slice(0, o)
    rest = rest.slice(o + 1)
    let c = rest.position("]")
    if c == none { buf += "[" + rest; rest = ""; break }
    let ch = rest.slice(0, c)
    rest = rest.slice(c + 1)
    if buf != "" or pending != none { segs.push((chord: pending, text: buf)) }
    pending = transpose(ch, semitones, prefer-flat: flat)
    buf = ""
  }
  if buf != "" or pending != none { segs.push((chord: pending, text: buf)) }
  if segs.len() == 0 { segs.push((chord: none, text: "")) }
  segs
}

#let parse-song(path, scores: none) = {
  let src = read(path)
  let meta = (:)
  let sections = ()
  let cur = none
  let in-head = true
  let grid-acc = ""

  let flush-grid() = {}

  for raw in src.split("\n") {
    let line = raw.trim()
    if line.starts-with("#") { continue }

    if line.starts-with("[") and (line.ends-with("]") or line.ends-with("] (repeat)")) {
      if cur != none { sections.push(cur) }
      let rep = line.ends-with("(repeat)")
      let lbl = if rep { line.slice(1, line.position("]")) } else { line.slice(1, -1) }
      cur = (label: lbl, grid: "", lines: (), per-line: 4, repeat: rep,
             score: none, time: none)
      in-head = false
      continue
    }

    if in-head {
      if line == "" { continue }
      let i = line.position(":")
      if i != none {
        let k = line.slice(0, i).trim()
        if k in META-KEYS { meta.insert(k, line.slice(i + 1).trim()) }
      }
      continue
    }

    if cur == none { continue }
    if line == "" { continue }

    if line.starts-with("time:") {
      cur.time = line.slice(5).trim()
    } else if line.starts-with("score:") {
      cur.score = line.slice(6).trim()
    } else if line.starts-with("grid:") {
      cur.grid += " " + line.slice(5).trim()
    } else if line.starts-with("bars:") {
      // Usually a uniform override of the 4-bars-per-line default (e.g. a
      // 6-bar verse written as one line). A comma list ("bars: 4,5") instead
      // gives the row sizes explicitly, for a form whose rows are not all the
      // same length -- a trailing bar that belongs on the previous system
      // rather than starting an orphan row of its own.
      let v = line.slice(5).trim()
      cur.per-line = if v.contains(",") { v.split(",").map(x => int(x.trim())) }
                     else { int(v) }
    } else if line.starts-with("|") and cur.lines.len() == 0 {
      // Grid lines always precede lyric lines within a section; once lyric
      // text has started, a line beginning with "|" is lyric content (an
      // informal beat marker some charts use), not a new grid row.
      cur.grid += " " + line
    } else {
      cur.lines.push(line)
    }
  }
  if cur != none { sections.push(cur) }

  let semitones = int(meta.at("transpose", default: "0"))
  let key = meta.at("key", default: "")
  // the source's own spelling wins; the key is only a fallback
  let flat = if "flats" in meta and meta.flats == "yes" { true }
             else if key != "" and (key in FLAT-KEYS or key.contains("b")) { true }
             else { auto }

  (
    ..meta,
    sections: sections.map(s => (
      label: s.label,
      per-line: s.per-line,
      repeat: s.repeat,
      score: s.score,
      time: s.time,
      // scores live beside the songs, in <book>/scores/
      score-path: if s.score == none { none } else {
        (if scores != none { scores } else {
          path.split("/").slice(0, -2).join("/").trim("/") + "/scores"
        }) + "/" + s.score
      },
      measures: if s.grid.trim() == "" { () } else { parse-grid(s.grid, semitones, flat) },
      lines: s.lines.map(l => parse-lyric(l, semitones, flat)),
    )),
  )
}
