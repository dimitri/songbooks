\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 84
  \set noChordSymbol = \markup { \beam #3 #0 #0.5 } 
  \repeat volta 4 { \bar ".|:"
    \alternative { \volta 1,2,3,4 { gis2 fis2 gis4 fis4 r2 } }
  } \break
  cis2 b2 cis4 b4 r2
  cis2 b2 d4 dis2. \break
  \repeat volta 2 { \bar ".|:" gis2 fis2 gis4 fis4 r2 }
}

\markup \vspace #1
