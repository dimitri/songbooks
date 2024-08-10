\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

<<

\chords { a2 c g1 a2 c g1 }

\new TabStaff {
   \tempo 4 = 100
   \tabFullNotation

   \repeat volta 5 { \bar ".:|"
     \alternative {
       \volta 1,2,3,4,5 {
         a,8 e a c4 e8 a e g,1
         a,8 e a c4 e8 a e g,1
       }
     }
   }
}

>>