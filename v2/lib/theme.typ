// Themes and target geometries.
//
// Page geometry follows MOLA's Music Preparation Guidelines: an A4 image area
// of 170 x 257 mm, i.e. 20 mm margins all round. That is not decoration --
// a page whose content block is exactly the MOLA image area renders at
// ~102-103% of print size on a 13" iPad once the reader crops the margins,
// against 88.5% for a full-bleed A4. An 11" iPad gets x0.764, so the type
// scale is sized for that worst case.

#let TARGETS = (
  // name: (paper w, paper h, margin, scale factor applied to the type scale)
  a4:       (w: 210mm, h: 297mm, margin: 20mm, k: 1.00),
  // Native-sized tablet pages: the PDF page IS the display area, so a point on
  // the page is a point on the screen and no scaling compensation is needed.
  // (The x0.764 penalty people hit is from displaying a full-bleed A4 on an
  // 11" screen -- which is exactly what these targets avoid.) Margins are
  // small because there is no binding and no hand-hold area.
  // 13" iPad Pro active area: 2064 x 2752 px at 264 ppi.
  tablet13: (w: 198.6mm, h: 264.8mm, margin: 10mm, k: 1.00),
  // 11" iPad active area: 1668 x 2388 px at 264 ppi. A physically smaller page
  // simply holds less at a legible size -- expect more songs to need editing.
  tablet11: (w: 160.5mm, h: 232.8mm, margin: 7mm, k: 1.00),
)

#let LIGHT = (
  bg:    white,
  ink:   rgb("#141414"),
  chord: rgb("#8a1c1c"),
  faint: rgb("#9a9a9a"),
  rule:  rgb("#141414"),
  chip:  rgb("#e8e8e8"),
  chip-ink: rgb("#141414"),
  meta:  rgb("#5a5a5a"),
)

// Dark theme for stage use. Palette taken from Other Lies' own title pages
// (logobg RGB 15,15,15 / bandbg RGB 10,22,22). Strokes are thickened against
// the light theme because inverted hairlines bloom and vanish on an emissive
// panel; chords stay at full contrast because they are the glance target.
#let DARK = (
  bg:    rgb("#0f0f0f"),
  ink:   rgb("#ededed"),
  chord: rgb("#e8a33d"),
  faint: rgb("#6e6e6e"),
  rule:  rgb("#b4b4b4"),
  chip:  rgb("#2b2b2b"),
  chip-ink: rgb("#ededed"),
  meta:  rgb("#9a9a9a"),
)

// Font stacks, not single names, so a CI build on Linux renders the same as a
// local one on macOS. PT Sans is the target: humanist, large x-height, open
// apertures, unambiguous 0/O and b/d -- the legibility triad that matters at
// 70-90 cm. It is also libre (SIL OFL), so CI can install it. The later
// entries are only there to keep a build legible if nothing else is present.
// Lato is vendored directly into fonts/ (SIL OFL) rather than assumed to be
// installed, so this renders identically on any machine -- local or CI --
// regardless of what system fonts happen to be present. Its Regular cut has
// visibly lighter, more open strokes than PT Sans at lyric size (17-21pt),
// which is what "not too bold" actually meant: PT Sans's default weight
// reads heavy at that size even though nothing explicitly bolds it.
// Title used to prefer Avenir Next, which is Apple-licensed and not vendored --
// it rendered fine locally but would have silently fallen back to something
// else on CI or any non-Mac machine, which is exactly the inconsistency the
// vendored-Lato move for lyric/chord was meant to close. Lato Bold carries a
// 26-42pt title just as well and keeps the whole sheet on one reproducible
// family.
#let FONTS = (
  lyric: ("Lato", "PT Sans", "Source Sans 3", "DejaVu Sans", "Liberation Sans"),
  chord: ("Lato", "PT Sans", "Source Sans 3", "DejaVu Sans", "Liberation Sans"),
  title: ("Lato", "Avenir Next", "PT Sans", "DejaVu Sans", "Liberation Sans"),
)

// Type scale, in points at k = 1.0. Floor is 14pt: below that the sheet fails
// at 80 cm by the arc-minute, proportional and x-height methods alike.
#let SCALE = (
  floor: 14pt,
  lyric-target: 18pt,
  lyric-max: 21pt,
  chord-ratio: 1.15,   // chords ride above the lyric size
  title: 26pt,
  meta: 13pt,
  chip: 14pt,
)
