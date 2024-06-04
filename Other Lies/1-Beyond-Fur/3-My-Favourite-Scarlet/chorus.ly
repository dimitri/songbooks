\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   \repeat volta 3 { \bar ".|:"
     \alternative {
       \volta 1,2 { d1 b:m fis:m e } \break
       \volta 3 { d1 b:m fis:m fis:m }
     }
   }
}
