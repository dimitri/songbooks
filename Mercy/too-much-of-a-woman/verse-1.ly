\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 92
  \repeat volta 4 { \bar ".|:"
    \alternative {
      \volta 1,2,3,4 { b1 a2 e }
    }
  }
}
