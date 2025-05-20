\include "header.ly"

\markup \vspace #1

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   \repeat volta 3 { \bar ".|:"
     b1:m d:maj7 g g     
   }
}

\markup \vspace #1
