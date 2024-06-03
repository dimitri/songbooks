\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 140
   \repeat volta 3 { \bar ".|:"
     \alternative {
       \volta 1,2,3 { fis:m1 fis:m a e }
     }
   }
}
