\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  d1:m7 f:maj7 bes:maj7 ees:79
  d2:m7 aes:maj7 g:m7 c:47
}
