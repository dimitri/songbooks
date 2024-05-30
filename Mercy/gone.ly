\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 62
  \repeat volta 2 { \bar ".|:"
    c1:m g g c:m \break aes ees g c:m
  }
}
