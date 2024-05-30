\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 62
 \repeat volta 2 { \bar ".|:"
   b1:m7 b:m7 b:m7 b:m7 \break
   e:m7 e:m7 b:m7 b:m7 \break
   fis:m7 e:m7 b:m7 b:m7
 }
}
