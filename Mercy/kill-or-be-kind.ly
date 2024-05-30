\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 88
  \repeat volta 4 {  \bar ".|:" c1:m c:m aes g }
}
