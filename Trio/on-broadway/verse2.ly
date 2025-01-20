\include "header.ly"

\markup \vspace #1

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \set noChordSymbol = \markup { \beam #3 #0 #0.5 } 
  \repeat volta 4 { \bar ".|:"
    \alternative { \volta 1,2,3,4 { a2 g2 a4 g4 r2 } }
  } \break
  d2 c2 d4 c4 r2
  d2 c2 d4 e2. \break
  \repeat volta 2 { \bar ".|:" a2 g2 a4 g4 r2 }
}

