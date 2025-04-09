\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  c1:9 c:9 g g a:7 a:7 d:sus2 d:sus2
}
