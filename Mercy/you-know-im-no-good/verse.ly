\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 100
  \repeat volta 3 {
    \alternative {
      \volta 1,2 { d1:m g:m a:7 d:m } 
      \volta 3 { g:m g:m e:7 e:7 f f e2:7 a:7 }
    }
  }
}
