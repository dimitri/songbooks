\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 83
  \repeat volta 4 {  \bar ".|:"
    d1:m7 g/d c2/d g2/d d:m7 \break
    d1:m7 g/d c2/d g2/d d:m7 \break

  }
}
