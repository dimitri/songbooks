\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 83
  \repeat volta 4 {  \bar ".|:"
    d1:m7 g:m7 g:m7
    \alternative {
      \volta 1 { a1:7 }
      \volta 2 { c1 c1 }
    }
  }
}
