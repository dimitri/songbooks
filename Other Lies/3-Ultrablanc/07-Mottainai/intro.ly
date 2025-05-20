\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 105
   \repeat volta 2 { \bar ".|:"
     b1:m b:m ais ais \break
     b1:m b:m ais ais \break
     g g e:m e:m \break
     g g e:m e:m \break
   }
}
