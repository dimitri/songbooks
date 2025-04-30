\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 115
  \repeat volta 4 {  \bar ".|:"
    b2:m g2 b:m g b:m g e:m b:m
  }
}
