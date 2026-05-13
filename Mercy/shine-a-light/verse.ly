\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 78
  \transpose c d {
    \repeat volta 2 { \bar ".|:" c1 c g g }
  }
}
