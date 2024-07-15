\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
 \tempo 4 = 94
 \repeat volta 2 { \bar ".|:"
   c1 c d:m d:m
 }
}
