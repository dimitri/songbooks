\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 3 { \bar ".|:"
     fis1:m e1:m fis:m e:m
  }
}
