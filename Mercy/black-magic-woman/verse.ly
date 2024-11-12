\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 130
  \repeat volta 4 {  \bar ".|:"
    d1:m d:m a:m a:m \break
    d:m d:m g:m g:m \break
    d2:m g:m7 c a d1:m d1:m
  }
}

\markup \vspace #1
