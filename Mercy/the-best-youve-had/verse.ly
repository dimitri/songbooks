\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 120
  \repeat volta 4 {  \bar ".|:"
    c1:m c:m f:m g
  }
}

\markup \vspace #1
