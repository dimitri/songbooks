\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 91
  \repeat volta 2 { \bar ".|:"
    a1:m d:sus2 f:maj7 g \break
    a1:m d:sus2 f:maj7 g \break
    c a:m f:maj7 c \break
    c a:m f:maj7 c   
  }
}

\markup \vspace #1
