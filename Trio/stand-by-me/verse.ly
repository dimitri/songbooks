\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 80
  \repeat volta 2 { \bar ".|:"
    g1 g e:m e:m \break
    c d g g
  }
}

\markup \vspace #1
