\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
 \tempo 4 = 120
 \set noChordSymbol = \markup { \beam #3 #0 #1 } 
 \repeat volta 2 { \bar ".|:"
   c1:m c:m c:m c:m \break
   f:m f:m c:m c:m \break
   g:m f:m c:m r1
 }
}
