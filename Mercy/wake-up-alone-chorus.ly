\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 4 { \bar ".|:"
    \alternative {
      \volta 1,2,3 { d1 d g e:9 } \break
      \volta 4 { c b:m dis:maj7 r }
    }
  }
}
