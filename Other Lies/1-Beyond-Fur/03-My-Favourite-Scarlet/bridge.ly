\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  fis:m1 fis:m fis:m fis:m \break
  fis:m fis:m gis b:m \break
  fis:m fis:m gis b:m 
}
