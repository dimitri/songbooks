\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 106
  \repeat volta 2 { \bar ".|:"
    f1:maj7 f:maj7 f1:maj7 f:maj7
    f1:maj7 f:maj7 g:m7 c:479
  }
}

\markup \vspace #1
