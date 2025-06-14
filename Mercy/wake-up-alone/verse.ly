\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
 \tempo 4 = 72
 \repeat volta 2 { \bar ".|:"
  a1 aes cis:m c \break
  e:maj7 cis:m c fis2:m f2
 }
}
