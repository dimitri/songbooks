\include "header.ly"

\paper {
  line-width = 8\cm
}

<<

\chords { b1:m a e e b1:m a e e }

\new TabStaff {
   \tabFullNotation
   \repeat volta 2 {
     b8\3 e' d' b\3 e' d' b\3 e'
     a e' cis' a e' cis' a e'
     gis2~ gis8 e gis a a b\3~ b2.\3
   }
   \repeat volta 2 {
     \relative c' {
       r8 fis8 d4 e8 d4 cis8 b4\3 b4.\3 a4.\3 gis2~
       gis8 e gis a a b~\3 b2.\3
     }
   }
}

>>
