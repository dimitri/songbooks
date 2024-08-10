\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

<<

\chords {
   c1:maj7 a:m9 c:maj7 a:m9 c:maj7 d
}

\new TabStaff {
   \tabFullNotation

   c8 e b\3 e'   c e b\3 e'   
   a,8 e b\3 c'  a,8 e b\3 c'
   
   c8 e b\3 e'   c e b\3 e'
   a,8 e b\3 c'  a,8 e b\3 c'
   
   c8 e b\3 e'   c e b\3 e'
   
   \set TabStaff.minimumFret = 3
   \set TabStaff.restrainOpenStrings = ##t
   d8 fis c' d'
   d8 fis c' d'
}

>>