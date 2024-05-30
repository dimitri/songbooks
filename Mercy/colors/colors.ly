\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 88
  \repeat volta 4 {  \bar ".|:" fis1:m fis:m a a \break e cis:m fis:m fis:m }
}
