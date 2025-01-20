\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 84
  \repeat volta 2 { \bar ".|:"
    fis2:m e:6 d:6 cis:7 \break
    fis2:m e:6 d:6 cis:7 \break
    d1:6 a2 cis2:7 \break
    d2 gis:m7 fis1:m \break
  }
}

\markup \vspace #1
