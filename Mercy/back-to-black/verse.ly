\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 64
  \repeat volta 2 { \bar ".|:"
    d1:m d:m g:m g:m bes bes a a
  }
}
