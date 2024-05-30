\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 60
   \repeat volta 2 { \bar ".|:"
     a1 a b:m b:m \break b:m b:m a a
   }
}
