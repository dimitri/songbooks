\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
 \tempo 4 = 120
 \set noChordSymbol = \markup { \beam #3 #0 #1 } 
 \repeat volta 2 { \bar ".|:"
   a1:m a:m a:m a:m \break
   d:m d:m a:m a:m \break
   e:m d:m a:m r1
 }
}
