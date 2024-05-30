\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   \repeat volta 2 { \bar ".|:"
     d1:7 d:7 d:7 d4:7 c bes a \break
     g2:m bes ees d:7 g1:m g:m }
}
