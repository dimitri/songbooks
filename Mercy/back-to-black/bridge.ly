\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  d1:m bes:maj7 f a \break
  d1:m bes:maj7 f a a:7
}
