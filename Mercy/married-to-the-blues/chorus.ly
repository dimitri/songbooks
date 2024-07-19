\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
 \repeat volta 2 { \bar ".|:"
   e1:m e:m b:m b:m \break
   fis:7 fis:7 b:m b:m \break
   e1:m e:m b:m b:m \break
   g g fis:7 fis:7
 }
}
