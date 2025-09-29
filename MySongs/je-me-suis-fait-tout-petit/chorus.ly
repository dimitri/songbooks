\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 { \bar ".|:"
    b1:m des2:7 fis2
    b2:m a2:7 d2 fis \break
    b1:m des2:7 fis2
    b2:m d2 g2 fis
  }
}

\markup \vspace #1
