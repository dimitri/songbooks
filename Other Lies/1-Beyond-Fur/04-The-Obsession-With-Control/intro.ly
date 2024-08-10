\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 130
  \repeat volta 2 { \bar ".|:"
     \repeat volta 5 {
       \alternative {
         \volta 1,2,3 { e1:m g2 b2:m }
         \volta 4     { a1:m a1:m }
         \volta 5     { c c }
      }
    }
  }
}
