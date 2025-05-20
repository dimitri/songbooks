\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   \tempo 4 = 80
   b1:m cis:m d e:m \break
   b1:m cis:m d e:m \break
   fis:m fis:m
}
