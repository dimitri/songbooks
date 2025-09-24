\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 { \bar ".|:"
    b1:m des2:7 fis2:7 
    b2:m a2:7 d2:7 fis:7 \break
    b1:m des2:7 fis2:7 
    b2:m d2:7 g2:7 fis:7 \break
    b2:m fis:7 b:m fis:7 
  }
}

\markup \vspace #1
