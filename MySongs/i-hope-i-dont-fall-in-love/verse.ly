\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 86
  \repeat volta 2 { \bar ".|:"
    c2 f2 f1/g g1:9 \break
    c2 f2 f1/g g1:9 \break
    f2 c g c f c/e \break
    f2 f1/g g1:9 c2 \break
    f4 g4 c2 c1 c1
  }
}

\markup \vspace #1
