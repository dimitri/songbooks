\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 105
  \repeat volta 12 { \bar ".|:"
     e1:m g:2 d:2
  }
}

\markup \vspace #1