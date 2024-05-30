\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 70
 \repeat volta 2 { \bar ".|:"
   d1 d g g \break d a d2 g d a
 }
}
