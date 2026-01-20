\include "header.ly"

\paper {
  line-width = 8\cm
}

<<

\chords  { e1 b1:m a e e b:m a e e b:m a e e }

\new TabStaff {
   \tabFullNotation
   \tempo 4 = 113
   r2. b4
   \relative c' {
     fis1~\2 fis2.\2 fis4\2 b,1~ b2. b4 
     fis'1~\2 fis2\2 e8.\2 d8. cis8 b2~ b8 e,16 fis a8 fis gis1
   }
   \repeat volta 2 {
     \relative c' {
       r8 fis8 d4 e8 d4 cis8 b4\3 b4.\3 a4.\3 gis2~
       gis8 e gis a a b~\3 b2.\3
     }
   }
}

>>
