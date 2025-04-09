\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 117
  \repeat volta 3 { \bar ".|:"
    g1 g e:m e:m c:9 d:sus2 
    \alternative {
      \volta 1,3 { g g }
      \volta 2 { e:m e:m } }
  }
}
