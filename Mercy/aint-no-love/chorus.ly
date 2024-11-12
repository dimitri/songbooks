\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 72
  \repeat volta 4 {  \bar ".|:"
    g2.:m c4:m g1:m \break
    ees2. f4 g1:m \break
    g2.:m c4:m g1:m \break
    ees2 c2:m g1:m 
  }
}
