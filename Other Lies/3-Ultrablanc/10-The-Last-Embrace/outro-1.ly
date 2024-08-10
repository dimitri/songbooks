\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 160
   e1:m e:m  
   \repeat volta 3 { \bar ".|:"
     \alternative {
       \volta 1,2 { e1:m e:m c c }
       \volta 3   { g g a:7 a:7 }
     }
   }
   c1 c \break
   \repeat volta 3 { \bar ".|:"
     \alternative {
       \volta 1,2 { e1:m e:m c c } \break
       \volta 3   { g g a:7 a:7 }
     }
   }
   c1 c
}
