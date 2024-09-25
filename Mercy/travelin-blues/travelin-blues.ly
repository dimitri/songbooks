\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 100
 \repeat volta 2 { \bar ".|:"
   a1:m a:m a:m a:m \break
   d:m d:m a:m a:m \break
   f e:m a:m a:m
 }
}
