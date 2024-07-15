\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  f1 e:m f e:m g g \break
  \repeat volta 2 { \bar ".|:"
   c1 c d:m d:m
 }
}
