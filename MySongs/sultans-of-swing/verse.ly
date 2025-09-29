\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 149
  \repeat volta 2 { \bar ".|:"
    d1:m c2 bes a1 a \break
    d1:m c2 bes a1 a \break
    f1 f c c \break
    bes bes d:m d2:m bes \break
    c1 c
  }
}

\markup \vspace #1
