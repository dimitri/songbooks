\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 65
  \repeat volta 4 { \bar ".|:"
    g1:m bes d:m c \break
    g:m bes f c
    \alternative { \volta 1,2,3 { }
                   \volta 4 { c }
    }
  }
}
