\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
 \tempo 4 = 120
 \repeat volta 4 { \bar ".|:"
   \alternative { \volta 1,2,3,4 { b1:m a e:m fis:7 } }
 }
 \break
 e:m e:m fis:7 fis:7
 \repeat volta 2 { \bar ".|:"
   b1:m a e:m fis:7
 }
}

\markup \vspace #1
