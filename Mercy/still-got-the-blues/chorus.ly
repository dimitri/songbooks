\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \set noChordSymbol = \markup { \beam #3 #0 #1 } 
  a2:m e:m a:m d:9
  f:9 e:7.9+ a:m r2
}
