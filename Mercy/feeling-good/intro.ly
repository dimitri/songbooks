\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 65
  \repeat volta 4 { \bar ".|:" a1:m c f e }
}

\markup \vspace #1
