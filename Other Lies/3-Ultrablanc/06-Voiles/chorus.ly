\include "header.ly"

\markup \vspace #1

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   \repeat volta 2 { \bar ".|:"
      e1:m e:m c:maj7
      \alternative {
        \volta 1 { c1:maj7 }
        \volta 2 { c2:maj7 b4:m a4:7 }
      }
      
   }
}

\markup \vspace #1
