\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \set noChordSymbol = \markup { \beam #3 #0 #1 }
  \repeat volta 6 {
    \alternative {
      \volta 1,2,3,4,5 { g2 c4 bes g2 c4 bes }
      \break
      \volta 6 { g2 c4 bes g1 }
    }
  }
  \break
  c1 c g g \break c c
}
