\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 103
  \repeat volta 4 {  \bar ".|:"
    a1:m7 d:6 a:m7 d:6
  }
  a:m7 g:m7 f:maj7 f:maj7
}
