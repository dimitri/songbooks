#import "/lib/chord.typ": show-chord
#import "/lib/theme.typ": LIGHT, FONTS
#set page(width: 210mm, height: 297mm, margin: 16mm, fill: white)
#set text(font: "PT Sans", size: 11pt, fill: black)

#let th = LIGHT
#let E = (staff-line: 0.13, thin: 0.16, thick: 0.50, sep: 0.40, dot-sep: 0.16, dot-r: 0.20)

// a neutral harmonic framework, not a particular tune
#let BARS = (("Dm7",), ("G7",), ("Cmaj7",), ("Cmaj7",))

#let sysbox(sp, above, below, body) = box(width: 100%, height: above + 4*sp + below, body)

#let lines(sp, above, w) = for i in range(5) {
  place(top + left, dy: above + i*sp, line(length: w, stroke: E.staff-line*sp + black))
}
#let barline(sp, above, x, thick: false) = place(top + left, dx: x, dy: above,
  line(length: 4*sp, angle: 90deg, stroke: (if thick { E.thick } else { E.thin })*sp + black))

#let slashes(sp, above, x, bw) = for j in range(4) {
  // slash notation: one per beat, spanning the 2nd-4th staff spaces
  place(top + left, dx: x + bw*(j + 0.5)/4 - 0.5*sp, dy: above + 1*sp,
    line(start: (0pt, 2*sp), end: (1*sp, 0pt), stroke: 0.3*sp + black))
}

#let variant(name, note, sp: 9pt, mode: "on") = {
  text(size: 12pt, weight: "bold", name)
  linebreak()
  text(size: 9.5pt, fill: rgb("#666"), note)
  v(0.5em)
  layout(av => {
    let w = av.width
    let above = if mode == "on" { 0.3*sp } else { 2.2*sp }
    let lead = 0.4*sp
    let n = BARS.len()
    let bw = (w - lead - E.thin*sp*(n+1)) / n
    sysbox(sp, above, 0.6*sp, {
      if mode != "none" { lines(sp, above, w) }
      let x = lead
      barline(sp, above, x)
      for (i, b) in BARS.enumerate() {
        if i > 0 { barline(sp, above, x) }
        x += E.thin*sp
        if mode == "on" {
          // chord sits where the noteheads would, knocked out of the lines
          place(top + left, dx: x + 0.3*sp, dy: above,
            box(height: 4*sp, align(horizon,
              box(fill: white, inset: (x: 0.42*sp, y: 0pt),
                text(size: 1.75*sp, fill: th.chord, show-chord(b.at(0)))))))
        } else {
          // chord above the staff, left-aligned to the barline
          place(top + left, dx: x + 0.25*sp, dy: above - 1.95*sp,
            text(size: 1.6*sp, fill: th.chord, show-chord(b.at(0))))
          if mode == "slash" { slashes(sp, above, x, bw) }
        }
        x += bw
      }
      barline(sp, above, x)
    })
  })
  v(1.6em)
}

#text(size: 19pt, weight: "bold")[Four ways to set a chord system]
#v(0.2em)
#line(length: 100%, stroke: 1pt)
#v(1em)

#variant("A — chords on the staff (what we have now)",
  "Chord names sit where noteheads would, knocked out of the lines. The staff carries no information; the lines read as debris around the text.",
  mode: "on")

#variant("B — chords above, slashes on the staff",
  "The standard jazz rhythm-chart idiom: one slash per beat says 'play time, voicing is yours'. The staff now does work, and the chords read as a clean band above it.",
  mode: "slash")

#variant("C — chords above, empty staff",
  "Same placement, no slashes. Quieter, but the empty staff is still inert.",
  mode: "empty")

#variant("D — barlines only, no staff",
  "What LilyPond's ChordNames + Bar_engraver gives, and what the current books use. Most compact of all; no notation feel.",
  mode: "none")
