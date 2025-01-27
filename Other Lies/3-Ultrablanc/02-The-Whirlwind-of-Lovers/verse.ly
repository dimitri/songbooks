\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 108
  \repeat volta 5 { \bar ".|:"
    \alternative { \volta 1,2,3,4,5 { g1:m g:m g:m g:m } }
  }
}

\markup \vspace #1
