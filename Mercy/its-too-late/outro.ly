\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  c1:maj7 f:maj7 bes1:maj7 a:m7 \break
  g:m7 f:maj7 d:m7 e:7          \break
  c1:maj7 f:maj7 c1:maj7 f:maj7 \break
  c1:maj7 c1:maj7
}
