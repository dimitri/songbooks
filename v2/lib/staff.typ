// A five-line staff carrying chord symbols above and rhythm slashes on it.
//
// This is the standard idiom for a chords-only performance chart: the staff is
// real, slashes mark the beats ("play time, voicing is yours"), and the chord
// symbols sit in a band above. An earlier version set the chord names where
// the noteheads go; that made the five lines carry no information and they
// read as debris around the text.
//
// All geometry is in STAFF SPACES (S). A five-line staff is 4 S tall, with the
// middle line at 2 S. Constants come from SMuFL's engravingDefaults (the
// machine-readable standard Bravura and every SMuFL font publish) and from
// LilyPond's Metafont sources, which are the only place the slash is specified
// as a construction rather than as a font outline.
//
// Deviations from the defaults, all deliberate, for a chords-only chart:
//   * chord symbols sit a measured 0.4 S clear of the top line (see
//     CHORD.gap-above) rather than the standard 2.5 S baseline-to-line --
//     nothing protrudes from the staff here, so the clearance the standard
//     reserves for high notes and ledger lines is dead space
//   * no clef and no key signature. A conventional chart repeats both on every
//     system, but there is no pitch content on this staff for them to qualify
//   * system gaps are tighter than a full score's 8.5-12 S, because the band
//     above each staff is already accounted for

#import "chord.typ": show-chord

// SMuFL engravingDefaults (Bravura 1.482), in staff spaces.
#let E = (
  staff-line: 0.13,
  thin-barline: 0.16,
  thick-barline: 0.50,
  barline-sep: 0.40,          // thin-thin, and thin-thick
  repeat-dot-sep: 0.16,       // dot edge to the inner thin line
  repeat-dot-diameter: 0.40,
  volta-line: 0.16,           // repeatEndingLineThickness
)

// LilyPond's slash construction (mf/feta-params.mf, mf/feta-macros.mf):
//   slash_slope := 1.7            -> 59.5 degrees from horizontal
//   slash_thick := 2/3 * 0.48 S;  a beat slash draws at 1.5x that = 0.48 S
//   height = 2 * (1 S + half a staff line) ~ 2.1 S, so it covers the 2nd and
//   4th staff lines completely
// One construction for all three slash shapes, so a beat slash, a whole-bar
// slash and a bar-repeat sign share a slant and a stroke and read as one
// family. Only the width and the fill differ.
#let SLASH = (
  slope: 1.7,          // dy/dx, i.e. 59.5 degrees
  half-height: 1.0,    // covers the 2nd and 4th staff lines
  stroke: 0.42,        // horizontal width of the stroke
  width: 2.3,          // default total horizontal extent, in staff spaces
)

// Extra gap inserted between beat-slash GROUPS within a bar -- e.g. "Gm . .
// Cm" reads as a 3-beat group then a 1-beat group, not four evenly spaced
// beats, so the chord change is visible in the slashes themselves, not just
// in the chord symbols above them. 0.55 S read as basically no gap at all
// once a dense bar's own width grew to fit its chord text at full scale
// (see the width-allocation note on `min-bar-w` below): the gap is a FIXED
// amount while the bar around it got much wider, so it shrank to a rounding
// error next to the slot spacing. Used by both the renderer (`GROUP-GAP`
// below) and the width predictor (`min-bar-w`'s own `GAP`) -- one constant,
// so the two can never silently drift apart the way two independent
// literal `0.55 * sp`s eventually would.
#let BEAT-GROUP-GAP = 1.3
// A bar's measured text minimum (see `min-bar-w`) can be narrower than a
// bar reads as comfortably -- a short symbol like "C" needs almost no
// width at all on its own terms, which read as a visibly pinched bar next
// to a dense neighbour once bar widths started tracking real text need
// instead of a flat meter share. This is a floor under that measurement,
// not a replacement for it.
#let VISUAL-MIN-W = 3.2

#let STAFF-SPACES = 4
#let BARS-PER-LINE = 4

// Chord symbols: em = 2.0 S gives a cap height of ~1.45 S with a typical text
// face (MuseScore sets 10pt Edwin against a 1.75mm space; Edwin's capHeight is
// 0.722 em). They are LEFT-aligned to the beat they fall on: engraving apps
// default to centring on the notehead, but professional jazz practice is to
// put the left edge flush with the beat, so a long symbol cannot leave the
// downbeat ambiguous.
// `reserve` sizes the band above the staff for LAYOUT purposes only (how
// much room the system box and the volta bracket above it need) -- it just
// has to be generously large enough, since any slack is harmless blank
// space. `gap-above` is the real typographic contract: the CLEAR gap between
// a chord symbol's own bottom edge and the staff's top line. It used to be a
// single fixed offset from the text box's top-left corner, sized against an
// assumed cap-height -- but Lato's ascent metric runs well above a plain
// cap-height (it reserves room for diacritics), so a box positioned that way
// sat its actual glyphs -- the open bowl of a "G", a dropped minor "m" --
// measurably lower than intended, overlapping the staff at the larger end of
// the size range (a roomy system like a single-chord-per-bar verse, which
// hits full scale). Measuring each symbol's own rendered height and placing
// its BOTTOM `gap-above` clear of the staff (see the chord-band loop below)
// is correct regardless of font metrics, weight, or which modifiers
// (minor/extension/bass) a given symbol carries.
#let CHORD = (em: 2.0, reserve: 2.6, gap-above: 0.4, min-gap: 0.5,
              bar-pad: 0.55)
#let VOLTA = (above: 0.5, hook: 1.3, number-scale: 0.8)

// Bar-repeat sign ("simile"): the same slash as everything else, with a dot in
// the space above-left and below-right. One centred in a bar means "play the
// previous bar again"; the same sign with a 2 above means "play the previous
// two".
#let SIMILE = (dot: 0.32, dot-offset: 0.78)

// Tempo mark: a quarter note drawn from primitives (a filled, slightly tilted
// notehead with a stem) so the sheet needs no music font installed.
#let TEMPO = (head-w: 1.25, head-h: 0.85, stem: 3.2, stem-w: 0.13)

// Rhythm-slash notation at sub-beat resolution: the same idiom LilyPond's
// `\new Voice \with { \consists Pitch_squash_engraver } \improvisationOn`
// produces -- pitch squashed to one line, notehead kept as a slash, duration
// notated with completely ordinary stems/flags/beams rather than a bespoke
// scheme (see the Notation Reference, "Displaying rhythms" and "Beams").
// Geometry sourced the same way as the rest of this file: SMuFL
// engravingDefaults for stem/beam thickness. Stem length is deliberately
// NOT the standard octave (~3.5 S) a full score would use -- this file's own
// header already rejects that standard's other headroom reservations
// ("nothing protrudes from the staff here, so the clearance the standard
// reserves... is dead space"), and a full-length stem reaches straight into
// the chord band immediately above (which sits only CHORD.gap-above =
// 0.4 S up from the staff). 1.8 S clears that band with a visible gap instead.
#let STEM = (thickness: 0.12, length: 1.8)
#let BEAM = (thickness: 0.5, gap: 0.35)
// A single-flag hook per sixteenth-level: the real "flag8thUp" silhouette
// from Bravura (see event-flag below for how it was sourced), so `reach`
// and `drop` are that glyph's own natural width and height, not guessed --
// it is a narrow, tall hook (width:height = 264:810 in the font, ~0.33:1),
// not the near-square wedge an earlier pass here had. `drop` covers most
// but not all of STEM.length, matching the font's own design intent that a
// bare bit of stem shows below the flag's tapered end.
#let FLAG = (reach: 0.55, drop: 1.6, stack: 0.85)
// Augmentation dot: Behind Bars' rule for a notehead sitting ON a staff line
// (true of every notehead here, since the whole system is one squashed
// line) is to drop -- raise, in our y-down coordinates -- the dot into the
// space above rather than let it sit on the line itself. 0.5 S is that
// space's own height; `right` is the gap from the notehead's own edge, not
// from its centre, so it does not visually tuck up against the slash.
// Checked directly against the reference engraving: the dot sits close --
// clearly separated, but closer than an earlier pass here had it.
#let DOT = (above: 0.5, right: 0.35, radius: 0.18)

#let staff-system(measures, sp, width, theme, time: none, tempo: none,
                  show-slashes: true) = {
  let h = STAFF-SPACES * sp
  let has-volta = measures.any(m => m.volta != none)
  // band above the staff: chord symbols, then volta brackets outside them
  let chord-top = CHORD.reserve * sp
  let above = chord-top + if has-volta { (VOLTA.above + VOLTA.hook) * sp } else { 0pt }
  // the tempo mark sits above everything else, on the first system only
  let tempo-h = if tempo != none { 2.2 * sp } else { 0pt }
  let above = above + tempo-h
  let below = 0.2 * sp

  // Horizontal budget. Barlines and the opening time signature are structural
  // and come off the top; the bars share out what is left equally.
  let n = measures.len()
  // A bar's width is proportional to its length: a 2/4 bar among 4/4 bars gets
  // half the room, which is what makes the meter change legible rather than
  // just an annotation.
  let beats-of(m) = {
    let sig = m.at("meter", default: none)
    if sig == none { sig = if time == none { "4/4" } else { time } }
    let p = sig.split("/")
    if p.len() == 2 { float(int(p.at(0))) * 4.0 / float(int(p.at(1))) } else { 4.0 }
  }
  let beats = measures.map(beats-of)
  let total-beats = beats.sum()
  let PLACEHOLDER = (".", "%", "%%", "R", "r", "/")
  // a meter change needs room for the signature itself
  let sig-count = measures.enumerate().filter(((i, m)) =>
    m.at("meter", default: none) != none).len()
  let lead = if time != none { 2.4 * sp } else { 0.3 * sp }
  let thin-w = E.thin-barline * sp
  // a repeat barline is thick + sep + thin + dot-sep + dot = 1.62 S
  let repeat-w = (E.thick-barline + E.barline-sep + E.thin-barline
                  + E.repeat-dot-sep + E.repeat-dot-diameter) * sp
  let extras = measures.map(m =>
    (if m.open { repeat-w } else { 0pt }) + (if m.close { repeat-w } else { 0pt })
  ).sum(default: 0pt)
  let usable = width - lead - thin-w * (n + 1) - extras - sig-count * 2.0 * sp

  // A bar with more simultaneous chord changes than its meter alone implies
  // needs more than an equal share of the line to stay legible: with chords
  // on an even beat grid, a chord on the LAST occupied beat has only
  // ~1/nbeats of the bar's own width before the barline, no matter how wide
  // the bar is overall -- that is a real geometric constraint of the grid,
  // not a guess, so it is measured directly from the chord text instead of
  // approximated with a per-chord budget. (Earlier passes here tried flat
  // per-chord constants, which either did nothing for the common
  // 2-chords-in-4-beats case or scaled with chord COUNT alone, so a single
  // dense bar sharing a short line with few neighbours grabbed nearly half
  // the row. A later pass computed this correctly but then floored every
  // bar at its plain proportional share before handing out the rest, which
  // guarantees there is never anything LEFT to hand out -- the shares sum to
  // `usable` by construction, so a dense bar's extra need can only be met by
  // uniformly shrinking the whole row, never by actually taking the slack a
  // one-chord bar does not need. A one-chord bar's true minimum -- just
  // enough for its own text, not a quarter of the bar -- is computed the
  // same way below, so that slack is real and reaches the bar that needs
  // it.)
  // Mirrors the per-beat renderer's own geometry below (`edges`/`slot`/
  // `beat-x`/GROUP-GAP) closely enough to invert it -- given a chord's
  // rendered width, solve for the bar-w that gives it just enough room,
  // rather than re-deriving an approximation that silently drifts from
  // whatever the renderer actually does. A bar with 2+ chord GROUPS (any
  // non-"." entries) inserts the same 0.55 sp GROUP-GAP between them that
  // the slash grouping uses, and the renderer nudges each chord's anchor
  // `slot * 0.12` off its raw beat position (see `beat-x`) -- both of those
  // are real costs this has to account for, not just the chord text itself.
  let min-bar-w(m) = {
    if m.events.len() > 0 or m.chords.len() == 0 { return 0pt }
    // A single chord filling the whole bar is a different shape from the
    // per-beat grid below: the renderer anchors it at beat 0 regardless of
    // the bar's true beat count and lets it run all the way to the
    // barline (see the `k == 1` branch at the pending.push call site), so
    // its own real constraint is just its text plus the bar's end pad --
    // not a quarter-bar slot the way a 4-way split implies.
    if m.chords.len() == 1 {
      if m.chords.at(0) in PLACEHOLDER { return 0pt }
      let nbeats = calc.max(1, int(calc.round(beats-of(m))))
      let w = measure(text(size: CHORD.em * sp, show-chord(m.chords.at(0)))).width
      // A short symbol's own text minimum (a bare "C" or "Gm") can come out
      // narrower than a bar reads as at a glance -- VISUAL-MIN-W is a floor
      // under the text requirement, not instead of it, so a one-chord bar
      // never looks pinched next to a dense neighbour just because its own
      // chord happens to be short.
      return calc.max(VISUAL-MIN-W * sp, (w + CHORD.bar-pad * sp) / (1 - 0.12 / nbeats))
    }
    let nbeats = m.chords.len()
    let occupied = range(nbeats).filter(j => m.chords.at(j) not in PLACEHOLDER)
    let ngroups = occupied.len()
    if ngroups == 0 { return 0pt }
    let GAP = if ngroups > 1 { BEAT-GROUP-GAP * sp } else { 0pt }
    let group-total = (ngroups - 1) * GAP
    let need = 0pt
    for k in range(ngroups) {
      let j = occupied.at(k)
      let w = measure(text(size: CHORD.em * sp, show-chord(m.chords.at(j)))).width
      if k + 1 < ngroups {
        // room before the NEXT occupied beat: a GROUP-GAP already sits
        // between them structurally, so only the shortfall past that
        // (if any) has to come from widening the bar
        let span = occupied.at(k + 1) - j
        let short = calc.max(0pt, w + CHORD.min-gap * sp - GAP)
        need = calc.max(need, nbeats * short / span + group-total)
      } else {
        // room before the barline for the last chord in the bar -- offset
        // by its own `slot * 0.12` anchor nudge, which (unlike the
        // inter-chord case) does not cancel out against anything
        // +1 sp of slack: the derivation above matches the renderer's own
        // geometry, but the shoved() scale search that actually places the
        // text (see further down) also charges a
        // `CHORD.min-gap` penalty against whatever the PREVIOUS symbol on
        // the line ended at, which this bar-local derivation has no way to
        // see -- a one-space margin keeps a same-bar collision from coming
        // down to that slop rather than to a genuine lack of room.
        let a = j + 0.12
        need = calc.max(need,
          (w + CHORD.bar-pad * sp) * nbeats / (nbeats - a) + group-total + sp)
      }
    }
    calc.max(VISUAL-MIN-W * sp, need)
  }
  let min-pt = measures.map(min-bar-w)
  let total-min = min-pt.sum(default: 0pt)
  // Split each bar's minimum into its VISUAL-MIN-W floor (what a one-chord
  // bar needs just to not look pinched -- small, and the same for every
  // such bar) and whatever it needs ABOVE that floor (a dense bar's own
  // text requirement, which is where all the real variation lives). The
  // floor is granted in full whenever the row can possibly afford it; only
  // the "extra" above it is what gets rationed when the row runs short.
  // Splitting it this way matters specifically when the row DOES run
  // short: scaling every bar's full minimum down by one shared factor (an
  // earlier version of this) shrinks a one-chord bar's already-small floor
  // right along with a dense bar's real text need, so the one bar this
  // whole mechanism exists to protect gets squeezed anyway, by exactly the
  // amount the dense bar overshoots. Keeping the floor off that scaling
  // entirely -- rationing only the extras among the bars that have them --
  // means a dense bar gives ground to ANOTHER dense bar before it ever
  // touches a one-chord bar's floor.
  // Every bar gets at least its own measured minimum; whatever room is left
  // over is handed out by meter proportion, same as always, so the row
  // still fills edge to edge and a 2/4 bar still reads as half a 4/4 bar's
  // width. A one-chord bar's minimum is small (see above), so in the
  // ordinary case there is plenty of slack left for a bar that actually
  // needs it -- that slack, not a flat per-chord bonus, is what lets a
  // dense bar reach the same type size as its roomier neighbours.
  let bar-widths-pt = if total-min <= usable {
    let slack = usable - total-min
    range(n).map(i => min-pt.at(i) + slack * beats.at(i) / total-beats)
  } else {
    // Bars' bare minimums together still exceed the row -- scale every
    // bar's own minimum down by the same factor instead of discarding
    // them for a flat meter share.
    //
    // Two alternatives were tried and rejected: protecting every bar's
    // VISUAL-MIN-W floor in full and rationing only the amount above it
    // (whether reserved from every bar or only from the ones that actually
    // need it) always makes a DENSE bar's type size worse than uniform
    // scaling does, not better -- fully protecting a one-chord bar's width
    // necessarily means the dense bars absorb 100% of whatever the row is
    // short, where uniform scaling spreads that shortfall across every bar
    // on the line, sparse ones included, so no single bar carries all of
    // it. A one-chord bar does give up a little of its own floor this way,
    // but that is the cheaper trade: a few points off a bar that was never
    // the reason anyone opened the chart is a smaller cost than legible
    // type on the bar that actually carries the chord changes.
    let k = usable / total-min
    min-pt.map(w => w * k)
  }

  box(width: width, height: above + h + below, {
    let y-top = above                 // y of the top staff line
    let y-mid = above + 2 * sp
    let y-bot = above + h

    // --- staff lines -------------------------------------------------------
    for i in range(5) {
      place(top + left, dy: y-top + i * sp,
        line(length: width, stroke: E.staff-line * sp + theme.rule))
    }

    // --- the slash, in its three uses --------------------------------------
    // `w` is the total horizontal extent; `hollow` gives the whole-bar form.
    let slash(xc, w, hollow: false, col: none) = {
      let c = if col == none { theme.ink } else { col }
      // The slant is FIXED at SLASH.slope for every slash on the page. Deriving
      // the run from the requested width instead made the angle a function of
      // the width, so a narrow beat slash came out visibly steeper than a wide
      // whole-bar one. When a slash has to fit a smaller box it is scaled
      // uniformly -- height, run and stroke together -- which leaves the angle
      // untouched.
      let hh0 = SLASH.half-height * sp
      let run0 = hh0 / SLASH.slope
      let sw0 = SLASH.stroke * sp
      let k = calc.min(1.0, w / (2 * run0 + sw0))
      let (hh, run, sw) = (hh0 * k, run0 * k, sw0 * k)
      let pts = (
        (xc + run - sw / 2, y-mid - hh), (xc + run + sw / 2, y-mid - hh),
        (xc - run + sw / 2, y-mid + hh), (xc - run - sw / 2, y-mid + hh),
      )
      place(top + left, dx: 0pt, dy: 0pt,
        if hollow { polygon(fill: none, stroke: 0.14 * sp * k + c, ..pts) }
        else { polygon(fill: c, stroke: none, ..pts) })
    }

    // Every use of the slash -- beat, whole-bar, bar-repeat -- shares this
    // width, so they read as one family and differ only in fill and dots.
    let wide-w(bw) = calc.min(SLASH.width * sp, bw * 0.5)

    // Full-bar rest: a "R" chord slot. Drawn as a standard whole-rest -- a
    // filled block hanging from the second line down -- rather than a slash,
    // since nothing is played in the bar at all; a hollow whole-bar slash
    // there would read as "one sustained chord", the opposite of silence.
    let rest(xc) = {
      let rw = 1.1 * sp
      let rh = 0.32 * sp
      place(top + left, dx: xc - rw / 2, dy: y-top + sp,
        rect(width: rw, height: rh, fill: theme.ink, stroke: none))
    }

    // --- rhythm-slash events (sub-beat duration grid) ----------------------
    // A compact eighth-rest-like glyph for any sub-beat rest: a short
    // diagonal stroke with a closed loop, legible at a glance rather than a
    // font trace. Quarter-and-longer rests reuse the whole-bar `rest` block
    // above, scaled to the event's own width.
    let event-rest(xc, w, dur) = {
      if dur >= 4 {
        let rw = calc.min(1.1 * sp, w * 0.7)
        let rh = 0.32 * sp
        place(top + left, dx: xc - rw / 2, dy: y-top + sp,
          rect(width: rw, height: rh, fill: theme.ink, stroke: none))
      } else {
        let s = 1.1 * sp
        let pts = (
          (xc - 0.08 * s, y-mid - 0.6 * s), (xc + 0.22 * s, y-mid - 0.1 * s),
          (xc - 0.05 * s, y-mid + 0.15 * s), (xc + 0.18 * s, y-mid + 0.1 * s),
          (xc - 0.22 * s, y-mid + 0.65 * s), (xc - 0.32 * s, y-mid + 0.55 * s),
          (xc + 0.02 * s, y-mid + 0.1 * s), (xc - 0.28 * s, y-mid - 0.15 * s),
        )
        place(top + left, polygon(fill: theme.ink, stroke: none, ..pts))
      }
    }
    // Notehead geometry, uniformly SCALED to fit the event's own slot --
    // exactly `slash()`'s own trick above (height, run and stroke together,
    // never just clamping width), so a dense run of eighths shrinks to a
    // smaller member of the same slash family instead of overflowing into
    // its neighbours at full quarter-note size. `w` is the slot's available
    // width (already shaved the way the old per-beat renderer shaves
    // `beat-w` -- see the call site); `scale-k` comes back out so the stem,
    // flag, beam and dot can shrink by the same factor and still look like
    // one note, not a full-size stem awkwardly grafted onto a small head.
    let event-geom(w) = {
      let hh0 = SLASH.half-height * sp
      let run0 = hh0 / SLASH.slope
      let sw0 = SLASH.stroke * sp
      let scale-k = calc.min(1.0, w / (2 * run0 + sw0))
      (hh: hh0 * scale-k, run: run0 * scale-k, sw: sw0 * scale-k, k: scale-k)
    }
    // Tip (top-right corner, where the stem attaches -- the natural
    // continuation of the slash's own slant) and the notehead itself, split
    // apart: a function whose body both shows content AND returns a plain
    // value makes Typst try to join them into one result, which fails for
    // content + dictionary (the same bug fixed in the volta bracket above).
    // Keeping geometry free of any place() call sidesteps it.
    let event-notehead-tip(xc, w) = {
      let g = event-geom(w)
      (x: xc + g.run + g.sw / 2, y: y-mid - g.hh, k: g.k)
    }
    let event-notehead(xc, w, hollow: false) = {
      let g = event-geom(w)
      let pts = (
        (xc + g.run - g.sw / 2, y-mid - g.hh), (xc + g.run + g.sw / 2, y-mid - g.hh),
        (xc - g.run + g.sw / 2, y-mid + g.hh), (xc - g.run - g.sw / 2, y-mid + g.hh),
      )
      place(top + left,
        if hollow { polygon(fill: none, stroke: 0.14 * sp * g.k + theme.ink, ..pts) }
        else { polygon(fill: theme.ink, stroke: none, ..pts) })
    }
    let event-stem-top(y0, k) = y0 - STEM.length * sp * k
    let event-stem(x, y0, k, to: none) = {
      let top-y = if to != none { to } else { event-stem-top(y0, k) }
      place(top + left, dx: x - STEM.thickness * sp * k / 2, dy: top-y,
        rect(width: STEM.thickness * sp * k, height: y0 - top-y, fill: theme.ink))
    }
    // `level` flags/beams: 1 for an eighth, 2 for a sixteenth. Stacked hooks
    // on an unbeamed note; stacked beam strokes (BEAM.gap apart) when shared
    // with neighbours.
    // The real "flag8thUp" outline from Bravura (SMuFL's open-source
    // reference font, SIL OFL), not a hand-guessed approximation: extracted
    // with fontTools (`glyphset["uniE240"].draw(SVGPathPen(...))`) and its
    // control points normalized to a unit box (ox,oy = the stem-tip anchor;
    // r,d = the glyph's own natural width/height, so FLAG.reach/FLAG.drop
    // just rescale it, same as every other primitive in this file). Still
    // drawn from primitives, not a font dependency -- this is the shape, not
    // a reference to the font itself.
    let event-flag(x, ytop, level, k) = for n in range(level) {
      let oy = ytop + n * FLAG.stack * sp * k
      let ox = x
      let r = FLAG.reach * sp * k
      let d = FLAG.drop * sp * k
      place(top + left, curve(
        curve.move((ox + 0.9015 * r, oy + 0.9753 * d)),
        curve.cubic((ox + 0.9015 * r, oy + 0.9753 * d), (ox + 1.0000 * r, oy + 0.8580 * d), (ox + 1.0000 * r, oy + 0.7617 * d)),
        curve.cubic((ox + 1.0000 * r, oy + 0.6074 * d), (ox + 0.8030 * r, oy + 0.4617 * d), (ox + 0.5644 * r, oy + 0.3383 * d)),
        curve.cubic((ox + 0.3712 * r, oy + 0.2407 * d), (ox + 0.2121 * r, oy + 0.1346 * d), (ox + 0.1515 * r, oy + 0.0160 * d)),
        curve.cubic((ox + 0.1402 * r, oy + -0.0037 * d), (ox + 0.1098 * r, oy + -0.0111 * d), (ox + 0.0720 * r, oy + -0.0111 * d)),
        curve.cubic((ox + 0.0303 * r, oy + -0.0111 * d), (ox + 0.0000 * r, oy + -0.0074 * d), (ox + 0.0000 * r, oy + 0.0074 * d)),
        curve.line((ox + 0.0000 * r, oy + 0.3025 * d)),
        curve.cubic((ox + 0.2500 * r, oy + 0.3173 * d), (ox + 0.6098 * r, oy + 0.4852 * d), (ox + 0.7462 * r, oy + 0.5901 * d)),
        curve.cubic((ox + 0.8030 * r, oy + 0.6321 * d), (ox + 0.8371 * r, oy + 0.7025 * d), (ox + 0.8371 * r, oy + 0.7753 * d)),
        curve.cubic((ox + 0.8371 * r, oy + 0.8309 * d), (ox + 0.8106 * r, oy + 0.8889 * d), (ox + 0.7462 * r, oy + 0.9444 * d)),
        curve.cubic((ox + 0.7386 * r, oy + 0.9519 * d), (ox + 0.7348 * r, oy + 0.9580 * d), (ox + 0.7348 * r, oy + 0.9630 * d)),
        curve.cubic((ox + 0.7348 * r, oy + 0.9827 * d), (ox + 0.7727 * r, oy + 0.9938 * d), (ox + 0.7955 * r, oy + 0.9988 * d)),
        curve.cubic((ox + 0.7992 * r, oy + 1.0000 * d), (ox + 0.8068 * r, oy + 1.0000 * d), (ox + 0.8144 * r, oy + 1.0000 * d)),
        curve.cubic((ox + 0.8409 * r, oy + 1.0000 * d), (ox + 0.8864 * r, oy + 0.9926 * d), (ox + 0.9015 * r, oy + 0.9753 * d)),
        curve.close(),
        fill: theme.ink, stroke: none,
      ))
    }
    let event-beam(x0, x1, ytop, level, k) = for n in range(level) {
      place(top + left, dx: x0, dy: ytop + n * (BEAM.thickness + BEAM.gap) * sp * k,
        rect(width: x1 - x0, height: BEAM.thickness * sp * k, fill: theme.ink))
    }
    // `edge-x` is the notehead's own right edge (its tip's x), not its
    // centre -- `right` is then a clean gap off that edge rather than
    // partly eaten by the notehead's own half-width.
    let event-dot(edge-x, k) = place(top + left,
      dx: edge-x + DOT.right * sp * k, dy: y-mid - DOT.above * sp * k - DOT.radius * sp * k,
      circle(radius: DOT.radius * sp * k, fill: theme.ink, stroke: none))
    // Flag/beam level for a duration in sixteenth-note units: 0 for a
    // quarter or longer, 1 for an eighth, 2 for a sixteenth. Durations this
    // can't classify (triplets, 32nds) simply get no flag -- not reachable
    // from today's ":N[.]" tag syntax (N in 1/2/4/8/16).
    let event-level(dur) = if dur >= 4 { 0 } else if dur >= 2 { 1 } else { 2 }
    // An explicit dot is shown for a dotted duration (dotted-eighth = 3,
    // dotted-quarter = 6, dotted-half = 12 sixteenths) -- the only dotted
    // values today's tag syntax can produce.
    let event-dotted(dur) = dur in (3, 6, 12)

    // Bar repeat: the same slash, plus the two dots.
    let simile(xc, w, count) = {
      slash(xc, w, hollow: true)
      for (dx, dy) in ((-SIMILE.dot-offset, -1.0), (SIMILE.dot-offset, 1.0)) {
        place(top + left,
          dx: xc + dx * sp - SIMILE.dot / 2 * sp,
          dy: y-mid + dy * sp - SIMILE.dot / 2 * sp,
          circle(radius: SIMILE.dot / 2 * sp, fill: theme.ink, stroke: none))
      }
      if count > 1 {
        place(top + left, dx: xc - 0.35 * sp, dy: y-top - 1.5 * sp,
          text(size: 1.2 * sp, weight: "bold", fill: theme.ink, str(count)))
      }
    }

    // --- barline primitives ------------------------------------------------
    // SMuFL is explicit that barlines and repeats should be drawn from
    // primitives rather than set as precomposed glyphs. A barline spans
    // exactly the staff height, bottom line centre to top line centre.
    let vrule(x, w) = place(top + left, dx: x, dy: y-top,
      rect(width: w, height: h, fill: theme.rule, stroke: none))
    let thin(x)  = vrule(x, E.thin-barline * sp)
    let thick(x) = vrule(x, E.thick-barline * sp)
    // the two dots sit in the 2nd and 3rd spaces, +/- 0.5 S from the middle
    // line, so neither can collide with a staff line
    let dots(x) = for dy in (1.5, 2.5) {
      place(top + left, dx: x, dy: y-top + dy * sp - E.repeat-dot-diameter / 2 * sp,
        circle(radius: E.repeat-dot-diameter / 2 * sp, fill: theme.rule, stroke: none))
    }

    // --- volta bracket -----------------------------------------------------
    // LilyPond's own convention (Notation Reference, "Long repeats"): a volta
    // bracket is drawn over the FULL WIDTH of every bar the ending covers, not
    // redrawn per bar. `run` is accumulated across consecutive measures that
    // share the same label and flushed (by the caller, passing the run value
    // explicitly -- a plain function argument rather than a captured mutable
    // closure, since it is drawn both mid-loop and once more after it ends).
    let draw-volta(run) = if run != none {
      let y = 0pt
      place(top + left, dx: run.x0, dy: y,
        line(length: run.x1 - run.x0, stroke: E.volta-line * sp + theme.rule))
      place(top + left, dx: run.x0, dy: y,
        line(length: VOLTA.hook * sp, angle: 90deg, stroke: E.volta-line * sp + theme.rule))
      place(top + left, dx: run.x0 + 0.6 * sp, dy: y + 0.25 * sp,
        text(size: CHORD.em * VOLTA.number-scale * sp, weight: "bold",
             fill: theme.ink, run.label))
    }

    // --- tempo mark --------------------------------------------------------
    // A quarter note drawn from primitives, so no music font is needed.
    if tempo != none {
      let hw = TEMPO.head-w * sp / 2
      let hh = TEMPO.head-h * sp / 2
      let bx = 0.5 * sp + hw
      let by = y-top - chord-top - 0.5 * sp
      place(top + left, dx: bx - hw, dy: by - hh,
        rotate(-20deg, origin: center + horizon,
          ellipse(width: 2 * hw, height: 2 * hh, fill: theme.ink, stroke: none)))
      place(top + left, dx: bx + hw - TEMPO.stem-w * sp, dy: by - TEMPO.stem * sp / 2,
        rect(width: TEMPO.stem-w * sp, height: TEMPO.stem * sp / 2,
             fill: theme.ink, stroke: none))
      place(top + left, dx: bx + hw + 0.5 * sp, dy: by - 0.95 * sp,
        text(size: 1.35 * sp, fill: theme.ink, "= " + tempo))
    }

    // --- time signature ----------------------------------------------------
    // SMuFL: each digit is two staff spaces tall, centred on the baseline.
    let timesig(at, sig) = {
      let parts = sig.split("/")
      place(top + left, dx: at, dy: y-top,
        box(height: h, align(horizon,
          text(size: 2.4 * sp, weight: "bold", fill: theme.ink,
            stack(spacing: -0.5 * sp,
              align(center, parts.at(0)), align(center, parts.at(1)))))))
    }
    if time != none { timesig(0.5 * sp, time) }

    // --- bars --------------------------------------------------------------
    let x = lead
    // (x, chord, limit, bar-left) chord-symbol placements for the WHOLE
    // system, one flat list -- not grouped per bar. A per-bar scale once
    // seemed like the fix for a crowded bar dragging its neighbours down
    // (see the old comment this replaced), but it trades that for a worse
    // problem: chord symbols at visibly different sizes bar to bar, with no
    // reason a reader can see -- it is not a musical distinction, so it
    // just reads as noise. One scale for the whole system -- the smallest
    // any bar on it actually needs -- means every symbol on a line is the
    // same size, and a crowded bar costs its neighbours a little size
    // rather than looking unrelated to them.
    let pending = ()
    thin(x)
    let running = time
    let volta-run = none     // (label, x0, x1): the open bracket, if any
    for (i, m) in measures.enumerate() {
      let bar-w = bar-widths-pt.at(i)
      // A bar immediately after a DIFFERENT volta label (ending 1 followed
      // by ending 2, ...) shares its left edge with the repeat-close barline
      // that mark (drawn below, at the end of the previous bar's own
      // processing) already provides -- see the note there for why that
      // barline belongs at this boundary rather than wherever `m.close`
      // happens to say.
      let prev-volta = if i > 0 { measures.at(i - 1).volta } else { none }
      let after-alt = prev-volta != none and m.volta != none and m.volta != prev-volta
      if m.open {
        thick(x + thin-w)
        dots(x + (E.thin-barline + E.thick-barline + E.barline-sep) * sp)
        x += repeat-w
      } else if i > 0 and not after-alt {
        thin(x)
      }
      x += thin-w
      // a meter change prints its signature just inside the barline
      let msig = m.at("meter", default: none)
      if msig != none and msig != running {
        timesig(x + 0.15 * sp, msig)
        x += 2.0 * sp
        running = msig
      }

      if m.events.len() > 0 {
        // --- rhythm-slash events: sub-beat durations (see song.typ's
        // ":N[.]" tag parsing), spaced by duration rather than one slash
        // per quarter-beat token. -------------------------------------
        // NOT linear in duration: measured directly against the reference
        // engraving (a dotted-quarter-then-eighth bar), the gap before the
        // eighth was ~1.87x the gap after it, not 3x -- a dotted quarter
        // (3 sixteenths) does not actually claim 3x an eighth's (1
        // sixteenth) width. That matches real spacing engines (LilyPond's
        // "springs", Gourlay's algorithm): space grows with roughly the
        // SQUARE ROOT of duration, not duration itself -- otherwise a long
        // note sprawls and a short one crowds its neighbours, which read as
        // "unreadable" rather than as one coherent rhythm. sqrt(3):sqrt(1)
        // = 1.73, close to what was actually measured.
        let weight(d) = calc.sqrt(d)
        let total-w = m.events.map(e => weight(e.dur)).sum()
        let ev-edges = {
          let acc = (0pt,)
          for e in m.events { acc.push(acc.last() + bar-w * weight(e.dur) / total-w) }
          acc
        }
        // One notehead size for the WHOLE bar, not one per event: standard
        // notation draws every notehead the same size regardless of
        // duration -- only the stem/flag/beam mark how long it rings.
        // Sizing each event to its own (very different) slot width drew a
        // dotted quarter's notehead much wider than the eighth beside it,
        // which read as a family of different-sized slashes rather than one
        // rhythm. The narrowest chord-bearing slot in the bar sets the
        // shared size, same budget the old per-beat renderer gives a beat
        // slash (`beat-w`): natural size, never wider than 72% of that
        // slot.
        let chord-slots = range(m.events.len()).filter(j => m.events.at(j).chord != none)
          .map(j => ev-edges.at(j + 1) - ev-edges.at(j))
        let bar-w-budget = if chord-slots.len() == 0 { SLASH.width * sp }
          else { calc.min(SLASH.width * sp, calc.min(..chord-slots) * 0.72) }
        // Beam groups: consecutive chord-bearing events at the same
        // flag level (both eighths, or both sixteenths -- not mixed).
        let levels = m.events.map(e => if e.chord == none { -1 } else { event-level(e.dur) })
        let groups = ()
        let gi = 0
        while gi < m.events.len() {
          if levels.at(gi) in (1, 2) {
            let gj = gi
            while gj + 1 < m.events.len() and levels.at(gj + 1) == levels.at(gi) { gj += 1 }
            if gj > gi { groups.push((gi, gj)) }
            gi = gj + 1
          } else { gi += 1 }
        }
        let beamed = (:)
        for (gi, gj) in groups { for ix in range(gi, gj + 1) { beamed.insert(str(ix), true) } }

        let tips = (:)
        let had-flag = false
        for (j, e) in m.events.enumerate() {
          let w-j = ev-edges.at(j + 1) - ev-edges.at(j)
          let xc = x + ev-edges.at(j) + w-j / 2
          if e.chord == none {
            event-rest(xc, w-j, e.dur)
            had-flag = false
            continue
          }
          // a half note or longer "lets ring" rather than marking a beat
          // hit -- same hollow-vs-filled distinction the whole-bar slash
          // already draws, at the same threshold (half note = 8 sixteenths)
          event-notehead(xc, bar-w-budget, hollow: e.dur >= 8)
          let tip = event-notehead-tip(xc, bar-w-budget)
          tips.insert(str(j), tip)
          if event-dotted(e.dur) { event-dot(tip.x, tip.k) }
          let level = event-level(e.dur)
          let draws-flag = str(j) not in beamed and level > 0
          if str(j) not in beamed {
            event-stem(tip.x, tip.y, tip.k)
            if level > 0 { event-flag(tip.x, event-stem-top(tip.y, tip.k), level, tip.k) }
          }
          // left-aligned to the event's own slot, same convention as the
          // per-beat renderer, rather than to the notehead's centre -- it
          // gives the shared shove/shrink pass an honest "natural" position
          // to push away from instead of starting every symbol pre-crowded
          // near its neighbours' noteheads. A flag reaches rightward past
          // its own notehead (see FLAG.reach), past the slot boundary this
          // position is otherwise based on, so the symbol right after a
          // flagged event gets nudged clear of it too.
          let flag-clearance = if had-flag { 1.6 * FLAG.reach * sp * tip.k } else { 0pt }
          pending.push((x + ev-edges.at(j) + flag-clearance, e.chord,
                        x + bar-w - CHORD.bar-pad * sp, x))
          had-flag = draws-flag
        }
        for (gi, gj) in groups {
          let level = levels.at(gi)
          // the smallest notehead in the group sets the scale for the whole
          // beam, so it never ends up thicker than the stem it's attached to
          let group-k = calc.min(..range(gi, gj + 1).map(ix => tips.at(str(ix)).k))
          let beam-y = event-stem-top(tips.at(str(gi)).y, group-k)
          let x0 = tips.at(str(gi)).x
          let x1 = tips.at(str(gj)).x
          event-beam(x0, x1, beam-y, level, group-k)
          for ix in range(gi, gj + 1) {
            let tip = tips.at(str(ix))
            event-stem(tip.x, tip.y, group-k, to: beam-y)
          }
        }
      } else {
      // --- rhythm slashes, one per beat, centred on the middle line --------
      let nbeats = calc.max(1, int(calc.round(beats.at(i))))
      // Group boundaries: a beat is a new group when its own slot holds a
      // chord name (not "."), i.e. a different chord than the one before it.
      let is-group-start = range(nbeats).map(j =>
        m.chords.len() == nbeats and m.chords.at(j) != ".")
      let ngroups = is-group-start.filter(x => x).len()
      let GROUP-GAP = if ngroups > 1 { BEAT-GROUP-GAP * sp } else { 0pt }
      let gap-total = (ngroups - 1) * GROUP-GAP
      let slot = (bar-w - calc.max(0pt, gap-total)) / nbeats
      // Cumulative left edge of each beat: beats in the same group sit at
      // plain slot spacing; crossing into a new group adds the extra gap, so
      // the two (or more) slashes under one chord read as a cluster and the
      // next chord's slashes read as a separate one.
      let edges = {
        let acc = (0pt,)
        for j in range(1, nbeats) {
          acc.push(acc.last() + slot + (if is-group-start.at(j) { GROUP-GAP } else { 0pt }))
        }
        acc
      }
      // Left edge of each beat's slash.
      let beat-w = calc.min(SLASH.width * sp, slot * 0.72)
      // Chord symbols anchor close to the slot's own start, not dead centre
      // on the slash glyph: centring (slot/2 - beat-w/2, the previous offset)
      // reads fine for an early beat, which has the rest of the bar to grow
      // into, but it also BURNS room no later beat can get back -- a chord
      // on the last beat of a 4-beat bar had only ~1/8 of the bar's own
      // width left before the barline, no matter how much total width the
      // bar was given, because 3.5 slots' worth of that width sat behind it
      // doing nothing for legibility. A small nudge (not the full slot/2)
      // keeps the symbol reading as "this beat, not the previous one" while
      // giving a late beat's chord close to its own full slot to grow into.
      let beat-x = range(nbeats).map(j => x + edges.at(j) + slot * 0.12)
      let is-simile = m.chords.len() > 0 and m.chords.at(0) in ("%", "%%")
      let is-rest = m.chords.len() == 1 and m.chords.at(0) == "R"
      if is-simile {
        simile(x + bar-w / 2, wide-w(bar-w),
               if m.chords.at(0) == "%%" { 2 } else { 1 })
      } else if is-rest {
        rest(x + bar-w / 2)
      } else if show-slashes and m.chords.len() == 1 and m.chords.at(0) != "." {
        // One chord filling the bar: a single whole-bar slash -- same shape and
        // width as the bar-repeat sign, hollow rather than filled.
        slash(x + bar-w / 2, wide-w(bar-w), hollow: true)
      } else if show-slashes {
        // A bar with several changes keeps one slash per quarter note, so the
        // chord above each beat has something to align to. These are never
        // suppressed: dropping them when the bar got narrow left the chords
        // floating over nothing.
        for j in range(nbeats) {
          slash(x + edges.at(j) + slot / 2, beat-w,
                col: if m.chords.any(c => c != ".") { theme.ink } else { theme.faint })
        }
      }

      // --- chord symbols, left-aligned to the beat they fall on ------------
      let k = m.chords.len()
      if not is-simile {
        for (j, c) in m.chords.enumerate() {
          if c == "." or c == "R" { continue }
          // a whole-bar chord belongs over beat 1; otherwise slot j is beat j
          // carry the bar's right edge: a symbol nudged right to clear its
          // neighbour must never cross into the next bar, or it reads as
          // belonging to that bar instead
          pending.push((beat-x.at(if k == 1 { 0 } else { calc.min(j, nbeats - 1) }),
                        c, x + bar-w - CHORD.bar-pad * sp, x))
        }
      }
      }

      // --- volta bracket, outside the chord band ---------------------------
      // Accumulated across consecutive bars that share the same ending label,
      // so the bracket spans all of them, not just the first -- the previous
      // version redrew an independent bracket per bar, which left a 2-bar
      // ending's second bar unbracketed.
      if m.volta != none {
        if volta-run != none and volta-run.label == m.volta {
          volta-run = (label: volta-run.label, x0: volta-run.x0, x1: x + bar-w)
        } else {
          // A plain statement, not `let _ = ...`: the latter would bind (and
          // so swallow) the content this returns instead of showing it --
          // the assignment just below it evaluates to `none`, which joins
          // with content for free, so nothing needs to absorb it.
          draw-volta(volta-run)
          volta-run = (label: m.volta, x0: x, x1: x + bar-w)
        }
      } else {
        draw-volta(volta-run)
        volta-run = none
      }

      x += bar-w
      // Where a repeat-close barline (thick + dots) actually belongs: a bar
      // that ends one volta ending and is followed by a DIFFERENT one is a
      // loop-back point -- "first time through, stop here and repeat; second
      // time, keep going into the next ending" -- regardless of what `m.close`
      // says. `m.close` itself is ignored for any volta-bearing bar: the
      // source data's own `close` flag comes from convert.py's brace-depth
      // tracking of LilyPond's `\repeat volta N { body \alternative { ... } }`,
      // which closes on whichever measure happens to be current when the
      // OUTER repeat's closing brace is reached -- that is always the LAST
      // alternative (the final ending), not the loop-back point, since the
      // whole `\alternative` block sits nested inside the repeat's own
      // braces. Deriving the barline from the volta labels themselves instead
      // is correct regardless of where that flag landed.
      let next-volta = if i + 1 < measures.len() { measures.at(i + 1).volta } else { none }
      let ends-alt = m.volta != none and next-volta != none and next-volta != m.volta
      if (m.close and m.volta == none) or ends-alt {
        dots(x + E.repeat-dot-sep * sp)
        thin(x + (E.repeat-dot-sep + E.repeat-dot-diameter + E.repeat-dot-sep) * sp)
        thick(x + (E.repeat-dot-sep + E.repeat-dot-diameter + E.repeat-dot-sep
                   + E.thin-barline + E.barline-sep) * sp)
        x += repeat-w
      }
    }
    draw-volta(volta-run)
    thin(x)

    // --- chord band: one sizing pass for the WHOLE SYSTEM, so every symbol
    // on the line comes out the same size (see the note on `pending` above)
    place(top + left, dx: 0pt, dy: 0pt, context {
      // Pick the largest chord size at which every symbol on the line still
      // sits over its own beat. Nudging a symbol right to clear its
      // neighbour stops them overlapping, but a bar with several changes
      // ends up with its chords shoved into a row, which loses the one
      // thing the placement is for. Shrinking the band is the better trade
      // -- uniformly, so the densest bar sets the size for bars that didn't
      // need it, rather than each bar picking its own.
      let shoved(scale) = {
        let pen = -1e9 * 1pt
        let bad = 0
        for (want, c, limit, left) in pending {
          let w = measure(text(size: CHORD.em * scale * sp,
                               show-chord(c))).width
          let at = calc.max(want, pen + CHORD.min-gap * sp)
          // too wide for the room left in its own bar counts as a miss, so
          // the size steps down rather than letting it spill across the
          // barline
          if at > want + 0.05 * sp or at + w > limit { bad += 1 }
          pen = at + w
        }
        bad
      }
      // The floor used to stop at 0.65 on the assumption that any system
      // would fit by then. A 3-chord split bar squeezed onto a forced
      // single-line system (e.g. `bars:` set higher than 4), or several
      // sub-beat events landing in one bar, can still collide there, and
      // the loop had nothing smaller left to try -- it kept 0.65 and let
      // the symbols overlap instead of shrinking further. Extending the
      // ladder costs nothing on the common case (the loop still breaks at
      // the first size that fits) and gives a genuinely dense line
      // somewhere left to go before giving up and overlapping anyway.
      let scale = 1.0
      for s in (1.0, 0.9, 0.8, 0.72, 0.65, 0.58, 0.5, 0.42, 0.36, 0.3) {
        scale = s
        if shoved(s) == 0 { break }
      }
      let pen = -1e9 * 1pt
      for (want, c, limit, bar-left) in pending {
        let body = text(size: CHORD.em * scale * sp, fill: theme.chord,
                        show-chord(c))
        let dims = measure(body)
        let w = dims.width
        // never past the bar's right edge, never before its left edge
        let at = calc.max(calc.min(calc.max(want, pen + CHORD.min-gap * sp),
                                   limit - w), bar-left)
        // BOTTOM-anchored, not top-anchored: a symbol's own measured height
        // (ascent+descent, as that specific font and weight actually render
        // it) sets how far up its box starts, so the clear gap to the staff
        // is exact regardless of which modifiers (minor, extension, bass)
        // pushed that particular symbol's ink lower or higher than another.
        place(top + left, dx: at, dy: y-top - CHORD.gap-above * sp - dims.height,
              body)
        pen = at + w
      }
    })
  })
}
