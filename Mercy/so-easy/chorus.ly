\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  f:maj7 bes:7 c:m7 e:m7.5- \break
  f:maj7 bes:7 c:m7 e:m7.5- \break
  f:maj7 bes:7 c:m7 a:m7.5- \break
  f:m11 f:m11 g:m7
}
