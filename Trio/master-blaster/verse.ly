\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 131
  \repeat volta 2 { \bar ".|:"
    a1:m g f e \break
    d d a:m g
  }
}

\markup \vspace #1
