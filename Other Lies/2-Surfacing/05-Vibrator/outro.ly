\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  d1:m d1:m d1:m d1:m \break
  d1:m d1:m d1:m d1:m
}
