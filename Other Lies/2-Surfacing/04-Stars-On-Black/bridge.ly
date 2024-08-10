\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  e1:m e:m e:m e:m \break

  \repeat volta 3 { \bar ".|:"
    \alternative {
      \volta 1,2,3 {
        e2:m g a:m f
        e2:m g a:m f
      }
    }
  }
}
