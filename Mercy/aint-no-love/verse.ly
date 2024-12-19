\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 {  \bar ".|:"
    g1:m ees f g:m
    g:m ees f ees2 bes
  }
}
