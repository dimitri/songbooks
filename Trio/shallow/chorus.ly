\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \repeat volta 2 { \bar ".|:"
    a1:m d
    g d e:m \break
    a1:m d
    g d e:m    
  }
}
