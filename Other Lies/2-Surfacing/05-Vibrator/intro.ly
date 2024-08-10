\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 113
  \repeat volta 2 { \bar ".|:" d1:m d:m d:m d:m }
}
