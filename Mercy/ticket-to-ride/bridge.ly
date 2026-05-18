\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  bes1:maj7 cis:maj7 g:m7 a:m7 \break
  bes1:maj7 cis:maj7 g:m7 c c:7
}
