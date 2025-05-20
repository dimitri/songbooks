\include "header.ly"

\markup \vspace #1

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   \repeat volta 2 { \bar ".|:"
      b2:m b8:m e4.:m
      b2:m b8:m e4.:m
      b2:m b8:m e4.:m
      b2:m b8:m e4.:m
   }
}
