\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 80
   \repeat volta 4 { \bar ".|:"
     e1:m e2:m a2
   }
}
