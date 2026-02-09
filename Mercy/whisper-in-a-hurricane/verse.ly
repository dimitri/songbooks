\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 79
  \repeat volta 4 {  \bar ".|:"
    a2:m7 g fis:m f4 g
  }
}
