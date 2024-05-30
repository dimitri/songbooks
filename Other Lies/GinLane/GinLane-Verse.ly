\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   \repeat volta 8 { \bar ".|:"
     \alternative {
       \volta 1,2,3,4,5,6,7,8 { a1:m e:m }
     }
   }
}
