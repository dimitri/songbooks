\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 160
   \repeat volta 2 { \bar ".|:" e1:m e:m c c } e1:m
}
