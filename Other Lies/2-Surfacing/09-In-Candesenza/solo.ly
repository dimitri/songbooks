\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   \repeat volta 7 { \bar ".|:"
     \alternative {
       \volta 1,2,4,6 { e1:m e1:m e1:m e1:m }
       \volta 3,5,7 { a1:m a:m a:m a:m }
     }
   }
}
