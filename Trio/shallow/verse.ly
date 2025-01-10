\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 96
  \repeat volta 2 { \bar ".|:"
    e2:m d2 g1 \break
    c1 g2 d2 \break
    e2:m g2 d1    
  }
}

\markup \vspace #1
