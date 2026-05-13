\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 { \bar ".|:"
    d2:m7 f/g c:maj7 f:maj7 \break
    \alternative {
      \volta 1 { b2:m7.5- e2 a1:m }
      \volta 2 { b1:m7.5- e2:sus4 e }
    }
  }
}
