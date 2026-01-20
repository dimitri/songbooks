\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 113
  \repeat volta 4 {  \bar ".|:"
    b1:m a e e
  }
}
