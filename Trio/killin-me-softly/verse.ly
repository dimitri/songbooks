\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 { \bar ".|:"
    a1:m7 d g \break
    a:m7 d e:m e:m \break
    a:m7 d:7 g b:7
  }
}

\markup \vspace #1