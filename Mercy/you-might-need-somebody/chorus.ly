\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 4 {  \bar ".|:"
    g1:m7 g:m7 a:m g:7
  }
}
