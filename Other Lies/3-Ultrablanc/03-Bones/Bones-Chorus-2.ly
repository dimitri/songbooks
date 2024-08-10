\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
   \repeat volta 2 { \bar ".|:"
     c1:maj7 c:maj7 a:m9 a:m9 \break
     c:maj7 c:maj7 a:m9 a:m9 \break
     c:maj7 c:maj7 d d d d
   }
}
