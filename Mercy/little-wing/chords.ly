\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 71
  \repeat volta 4 { \bar ".|:"
     e1:m g a:m e:m \break
     b2.:m bes4:m a2:m c \break
     g2 f c1 d d
  }
}
