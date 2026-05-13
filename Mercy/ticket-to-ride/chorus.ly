\include "header.ly"

\markup \vspace #1

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 { \bar ".|:"
    d1:m7 f:maj7 bes:maj7 ees:79 \break
    d2:m7 aes:maj7 g:m7 c:47 \break
    f:maj7 f:maj7
  }
}
