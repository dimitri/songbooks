\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  a1:m e:m a:m e:m \break
  a:m e:m e:m c2 d \break
  e1:m c2 d e1:m c2 d 
}
