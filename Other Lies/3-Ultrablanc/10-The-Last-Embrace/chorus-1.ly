\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   a1 a
   \repeat volta 4 { \bar ".|:"
     a1 a b:m b:m
   }
}
