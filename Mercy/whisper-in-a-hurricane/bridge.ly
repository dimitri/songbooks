\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 4 {  \bar ".|:"
    a2:m7 gis2 cis1:m
    a2:m7 gis2 cis1:m \break
    a2:m7 gis2 fis1:m f1 g1
  }
}
