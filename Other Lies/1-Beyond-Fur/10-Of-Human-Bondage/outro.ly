\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  fis:m b:m fis:m b:m \break
  fis:m b:m fis:m b:m 
}
