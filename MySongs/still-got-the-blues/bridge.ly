\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \set noChordSymbol = \markup { \beam #3 #0 #1 } 
  \transpose d b {
    b2:m e a4:m a:m/b c2
    b2:m e f4:maj7 e:m d2:m a:m r2
  }
}
