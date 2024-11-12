\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 76
  \repeat volta 4 {  \bar ".|:"
    e1:m e:m e:m b \break
    e:m e:m b2 a2 b2 a2 \break
    b2 a2 e1:m e1:m \break
  }
}
