\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 106
  f1:maj7 f:maj7 f1:maj7 f:maj7
}

\markup \vspace #1
