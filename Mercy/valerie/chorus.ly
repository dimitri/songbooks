\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 4 { \bar ".|:"
    \alternative {
      \volta 1,2,3 { aes1 g:m }
      \volta 4 { bes1 bes }
    }
  }
  \break
  \repeat volta 2 { \bar ".|:"
    ees:1 ees f:m f:m
 }
}
