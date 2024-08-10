\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 { \bar ".|:"
    \alternative {
      \volta 1,2 { a2 c g1 a2 c g1 }
    }
  }
}
