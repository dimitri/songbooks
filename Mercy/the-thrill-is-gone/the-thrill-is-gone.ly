\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 90
 \repeat volta 2 { \bar ".|:"
   b1:m b:m b:m b:m \break
   e:m e:m b:m b:m \break
   g:7 fis:7 b:m b:m
 }
}
