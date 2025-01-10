\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 92
  \repeat volta 2 { \bar ".|:"
    g1 b c c:m
  }
}

\markup \vspace #1
