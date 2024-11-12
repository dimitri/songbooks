\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 150
  cis1:7 cis:7 cis:7 cis:7 \break
  fis:7 fis:7 cis:7 cis:7 \break
  fis:7 fis:7 cis:7 cis:7 \break
  fis:7 fis:7  
}
