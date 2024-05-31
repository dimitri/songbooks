\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  fis1:m b:m fis1:m fis1:m \break
  b:m b:m cis cis
}
