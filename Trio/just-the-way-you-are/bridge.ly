\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 70
  \repeat volta 2 { \bar ".|:"
    g1:maj7 a:6 fis:m7 b:7 \break
    e:m7 a:7sus d d:/c \break
    bes c a:m7 d:9 \break
    g:m c:/g g:/a d
  }
}

\markup \vspace #1
