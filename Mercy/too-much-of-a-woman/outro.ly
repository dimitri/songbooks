\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 6 { \bar ".|:"
    \alternative {
      \volta 1,2,3 { b1 a2 e2 }
      \volta 4,5,6 { g1 a1 }
    }
  }
}
