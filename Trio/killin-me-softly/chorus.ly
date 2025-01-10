\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 92
  \repeat volta 2 { \bar ".|:"
    e1:m a:m d:7 g \break
    e:m a:m d c \break
    g c f:maj7 e
  }
}

\markup \vspace #1
