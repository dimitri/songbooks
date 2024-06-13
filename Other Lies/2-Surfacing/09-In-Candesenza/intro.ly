\include "header.ly"

<<

\chords  { e1:m fis:m g b:m7 }
\new TabStaff {
   \tabFullNotation
   \tempo 4 = 130
   \set TabStaff.minimumFret = 5
   \set TabStaff.restrainOpenStrings = ##t
   e4 g e8 g e16 fis8.~
   \set TabStaff.minimumFret = 7
   fis4 a fis8 a8 fis4
   \set TabStaff.minimumFret = 9
   g8 b e'4 g8 b e'4
   \set TabStaff.minimumFret = 7
   b4 d'8 a' b4 d'8 a'
}

>>
