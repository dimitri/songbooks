\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  cis1:7 gis:7 cis:7 fis:7 \break
  cis:7 dis:7 cis:7
}
