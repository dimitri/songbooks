\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 74
  \repeat volta 2 { \bar ".|:"
    b1:m fis a e \break
    g d e:m fis
  }
}

\markup \vspace #1
