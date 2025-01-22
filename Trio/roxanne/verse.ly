\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 133
  \repeat volta 2 { \bar ".|:"
    g1:m f:6 ees:maj7 d:m7 \break
    c:m7 f2.:sus g4:sus g1:sus
  }
}

\markup \vspace #1
