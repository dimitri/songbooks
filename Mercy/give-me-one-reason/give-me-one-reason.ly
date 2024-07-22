\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 80
  \repeat volta 4 {  \bar ".|:"
    fis1:7 b2:7 cis:7 fis1:7 fis:7 \break
    b1:7 b2:7 cis:7 fis1:7 fis:7 \break
    cis1:7 b:7 fis:7 fis:7
  }
}

\markup \vspace #1
