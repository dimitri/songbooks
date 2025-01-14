\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 96
  \set noChordSymbol = \markup { \beam #3 #0 #1 } 
  \repeat volta 2 { \bar ".|:"
    e2:m d4. g8 r1 \break
    c1 g2 d2
  }
}

\markup \vspace #1
