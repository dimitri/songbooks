\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 130
  \repeat volta 2 { \bar ".|:"
    b1:m b2:m e:m \break
    e2:m fis:7 b1:m \break
    b1:m e2:m fis:7 \break
    g:7 fis:7 b1:m \break
  }
}

\markup \vspace #1
