\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  bes1:maj7 f:maj7 bes1:maj7 f:maj7 \break
  bes1:maj7 f:maj7 d:m7 g:7
}
