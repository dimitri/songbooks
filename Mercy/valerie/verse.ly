\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
 \tempo 4 = 100
 \repeat volta 2 { \bar ".|:"
   ees1 ees f:m f:m
 }
}
