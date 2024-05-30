\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 64
  \repeat volta 2 { \bar ".|:" g2:m bes ees d:7 }
}
