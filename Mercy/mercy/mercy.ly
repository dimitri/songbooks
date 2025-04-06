\include "header.ly"

<<

\new Voice \with { \omit StringNumber} {
  \key bes \major
  \clef "treble_8"
  \tempo 4 = 90
  \relative {
    r2 r4
    <bes, bes'>8 <bes bes'> <d d'> <d d'> <ees ees'> <ees ees'> <f f'>2
  }
}
\new TabStaff {
  \tabFullNotation
  \relative {
    \set TabStaff.minimumFret = 5
    \set TabStaff.restrainOpenStrings = ##t
    r2 r4
    <bes, bes'>8 <bes bes'> <d d'> <d d'> <ees ees'> <ees ees'>
    \set TabStaff.minimumFret = 8
    <f f'>2
  }
}

>>