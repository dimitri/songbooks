\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  d1:m d:m d:m d:m \break
  f f f f \break
  f f d:m d:m
}
