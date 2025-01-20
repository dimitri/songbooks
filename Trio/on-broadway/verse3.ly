\include "header.ly"

\markup \vspace #1

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \set noChordSymbol = \markup { \beam #3 #0 #0.5 } 
  \repeat volta 4 { \bar ".|:"
    \alternative { \volta 1,2,3,4 { bes2 aes2 bes4 aes4 r2 } }
  } \break
  ees2 des2 ees4 des4 r2
  ees2 des2 ees4 e2:m f4 \break
  \repeat volta 2 { \bar ".|:" bes2 aes2 bes4 aes4 r2 }
}

