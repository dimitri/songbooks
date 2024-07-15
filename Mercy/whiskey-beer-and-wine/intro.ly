\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\new TabStaff {
   \tabFullNotation
   \tempo 4 = 94
   \set TabStaff.minimumFret = 8
   \set TabStaff.restrainOpenStrings = ##t
   \set Timing.beamExceptions = #'()

   c4 c'16 bes g f~ f8 ees8~ \grace f16( \glissando g8)
   
   \set TabStaff.minimumFret = 10
   ees'8 c'4 r2.

   \set TabStaff.minimumFret = 8
   c4 c'16 bes g f~ f8 ees8~ \grace f16( \glissando g8)

   \set TabStaff.minimumFret = 10
   bes8 c'4 r2.
}

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  c1:m c:m c:m c:m \break
  f:9 f:9 c:m c:m \break
  ees f c:m c:m
}
