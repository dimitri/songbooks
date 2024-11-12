\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 176
  \repeat volta 4 {  \bar ".|:"
    c1 c c c c c c c \break
    f f f f c c c c \break
    g:7 g:7 f f c c c c \break
    g g
  }
}
