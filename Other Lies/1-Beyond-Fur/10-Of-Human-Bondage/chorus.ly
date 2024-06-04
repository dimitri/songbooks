\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  e1 e e e \break
  b:m b:m d d \break
  fis:m fis:m e e \break
  e e 
}
