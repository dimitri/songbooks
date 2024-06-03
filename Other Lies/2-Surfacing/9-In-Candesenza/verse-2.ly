\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   \repeat volta 6 { \bar ".|:"
     \alternative {
       \volta 1,2,3,4,5,6 { e1:m fis:m g b:m7 }
     }
   }
}
