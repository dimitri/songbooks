\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 78
  \repeat volta 4 {  \bar ".|:"
    fis2:m7 b:9
    \alternative {
      \volta 1 { cis1:m7 }
      \volta 2 { cis2:m7 d2:7 }
    }
  }
}
