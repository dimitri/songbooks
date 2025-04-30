\include "header.ly"

\markup \vspace #1

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 4 {  \bar ".|:"
    d2 a g d \break
    b:m g
    \alternative {
      \volta 1 { b:m b:m }
      \volta 2 { e:m b:m }
    }
  }
}
