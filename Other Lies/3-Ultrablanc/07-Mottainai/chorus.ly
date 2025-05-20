\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   \repeat volta 2 { \bar ".|:"
      a1:9/cis a1:9/cis c:maj7 c:maj7
   }
}
