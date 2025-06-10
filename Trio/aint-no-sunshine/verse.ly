\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 74
  \repeat volta 2 { \bar ".|:"
    a1:m7 a2:m7 e4:m7 g4
    a1:m7 a2:m7 e4:m7 g4
    a1:m7 e1:m7 d1:m7 a2:m7 e4:m7 g4
  }
}

\markup \vspace #1
