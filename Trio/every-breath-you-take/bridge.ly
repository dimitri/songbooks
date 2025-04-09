\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 { \bar ".|:"
    g1 g e:m e:m c:9 d:sus2 
    \alternative {
      \volta 1 { e:m e:m }
      \volta 2 { g g }
    }
  }
}
