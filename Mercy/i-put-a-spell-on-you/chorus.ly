\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  fis1:m fis1:m b:m b:m \break
  fis:m d2 cis2 fis1:m d2 cis2
}
