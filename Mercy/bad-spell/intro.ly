\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\new TabStaff {
   \tabFullNotation
   \tempo 4 = 84
   \set TabStaff.minimumFret = 3
   \set TabStaff.restrainOpenStrings = ##t

   g,4 <d' g'>8. g,16 bes,8 c d f16 g
   g,4 <d' g'>8. bes16( c'4) bes8 \tuplet 3/2 { g16 f d } g,4
}

\markup \vspace #1
