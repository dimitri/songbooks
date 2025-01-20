\include "header.ly"

\markup \vspace #1

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 { \bar ".|:"
    a1:m7 d g c \break
    a:m7 d e:m e:m \break
    a:m7 d:7 g b:7
  }
}

