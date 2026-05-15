\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 54
  \transpose d b {
    d2:m7 f/g c:maj7 f:maj7
    b:m7.5- e a:m a:m
  }
}

\markup \vspace #1
