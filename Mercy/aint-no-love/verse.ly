\include "header.ly"

\markup \vspace #1

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 {  \bar ".|:"
    g1:m ees f \alternative { \volta 1 { g:m } \volta 2 { ees2 bes } }
  }
}
