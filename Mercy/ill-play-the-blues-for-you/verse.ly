\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 94
  \repeat volta 4 {  \bar ".|:"
    g1:m g:m g:m g:m \break
    c:m c:m g:m g:m \break
    bes d c:m c:m \break
    g:m g:m
  }
}
