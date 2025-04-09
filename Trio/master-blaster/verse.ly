\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 131
  \repeat volta 2 { \bar ".|:"
    c1:m c2:m bes2 aes1 aes2 g2 \break
    f1 f1 c:m bes
  }
}

\markup \vspace #1
