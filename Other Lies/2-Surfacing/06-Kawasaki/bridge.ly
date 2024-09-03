\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 4 {
    \alternative {
      \volta 1,2,3 { d2 e2 d1 }
      \volta 4 { d2 e2 fis1 }
    }
  }
}
