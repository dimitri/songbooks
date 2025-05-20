\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

<<

\chords {
   b1:m d:maj7 g g
}

\new TabStaff {
   \tabFullNotation
   \tempo 4 = 84

   b,16 fis d' b,  fis d'8 d'16   
   b,16 fis d' b,  fis d'8 d'16   

   \set TabStaff.minimumFret = 3
   \set TabStaff.restrainOpenStrings = ##f
   d16\5 a fis' d\5 a fis'8 fis'16
   d16\5 a fis'8 e'\2 d'\2

   \set TabStaff.minimumFret = 2
   \set TabStaff.restrainOpenStrings = ##f
   g,16 d\5 b\3 g, d\5 b8\3 b16\3
   g,16 d\5 b\3 g, d\5 b8\3 b16\3

   g,16 d\5 b\3 g, d\5 b8\3 b16\3
   g,16 d\5 b8\3 a\3 b\3
}

>>