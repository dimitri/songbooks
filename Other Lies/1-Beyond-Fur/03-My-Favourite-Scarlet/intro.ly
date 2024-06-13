\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 140
   \repeat volta 6 { \bar ".|:"
     \alternative {
       \volta 1,2,3 { fis1:m fis:m a e }
       \volta 4,5,6 { fis1:m fis:m a e }
     }
   }
}
