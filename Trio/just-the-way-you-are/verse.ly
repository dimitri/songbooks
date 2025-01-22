\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 70
  \repeat volta 2 { \bar ".|:"
    d1 b:m g:maj7 b2:m d2:7 \break
    g1:maj7 g:m7 d:/fis a2:m7 d2:7 \break
    g1:maj7 g:m7 d:/fis b:m7 \break
    e:9sus4 e:9 g:/a d
  }
}

\markup \vspace #1
