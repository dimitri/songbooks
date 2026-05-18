\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 4 { \bar ".|:"
    bes1:11 bes:11 ees:maj7 ees2:maj7 e:7.5-
  }
}
