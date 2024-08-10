\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 3 { \bar ".|:"
    \alternative {
      \volta 1,2 { bes1 g:m d:m f }
      \volta 3   { bes1 g:m d:m d:m }
    }
  }
}
