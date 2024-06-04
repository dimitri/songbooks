\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 5 { \bar ".|:"
    \alternative {
      \volta 1,2,3,4 { d e fis:m a }
      \volta 5 { d e }
    }
  }
}
