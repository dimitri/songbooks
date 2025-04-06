\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
 \tempo 4 = 72
 \repeat volta 2 { \bar ".|:"
  a1 a aes aes \break
  cis:m cis:m c c \break
  e:maj7 e:maj7 cis:m cis:m \break
  c c fis:m f
 }
}
