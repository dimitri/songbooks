\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  c1:maj7 f:maj7 bes1:maj7 a:m7
  g:m7 f:maj7 d:m7 e:7 \break
  \repeat volta 3 {  \bar ".|:"
    a:m7 d:7
    \alternative { \volta 3 { g:7 } }
  }
}
