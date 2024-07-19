\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
 \tempo 4 = 120
 \repeat volta 2 { \bar ".|:"
   b1:m a e:m b:m \break
   a g a b:m
 }
}
