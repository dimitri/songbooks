\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 96
  \repeat volta 2 { \bar ".|:"
    e1 e e b \break
    e a e2 b2 e1
  }
}
