\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 { \bar ".|:"
    a1:m g2 d:m a1:m b2:m d:m \break
    a2 c g1 a2 d g1
  }
}
