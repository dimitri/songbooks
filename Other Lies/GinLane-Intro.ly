\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 80
   \repeat volta 6 { \bar ".|:"
     \alternative {
       \volta 1,2,3,4,5,6 { a1:m e:m }
     }
   }
   \repeat volta 8 { \bar ".|:"
     \alternative {
       \volta 1,2,3,4,5,6,7,8 { a1:m e:m }
     }
   }
}
