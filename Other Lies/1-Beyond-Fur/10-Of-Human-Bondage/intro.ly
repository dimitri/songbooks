\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 120
  \repeat volta 4 { \bar ".|:"
     b1:m b:m b:m b:m \break
     fis:m1 fis:m fis:m fis:m
  }
}
