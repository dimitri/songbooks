\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \set noChordSymbol = \markup { \beam #3 #0 #1 }
  \repeat volta 4 { \bar ".|:"
    aes2 g
    \alternative {
     \volta 1,2,3 { c2:m ees }
     \volta 4 { r2 }
   }
  }
}
