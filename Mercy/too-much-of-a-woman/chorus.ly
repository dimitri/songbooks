\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 4 { \bar ".|:"
    \alternative {
      \volta 1,2,3 { b1 a2 e2 }
      \volta 4 { g1 a1 }
    }
  }
  \break
  b1 a2 e2 b1 a2 e2
}
