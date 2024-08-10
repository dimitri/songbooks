\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
 \repeat volta 2 { \bar ".:|" g1:m g1:m bes bes }
}
