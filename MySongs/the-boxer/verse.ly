\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 92
  c1 c2 a:m g1 g2:7 g:6 \break
  c1 c2 a2:m g2 f2 f2 c2 \break
  g1 f2 g2 c1
}
