\include "header.ly"

\new TabStaff {
   \tabFullNotation
   \tempo 4 = 96
   \set TabStaff.minimumFret = 0
   \set TabStaff.restrainOpenStrings = ##f

   <b, e gis>8. <gis b e'>16 r16 e, g, a, e,8. <gis b e'>16 r16 r16 g, d
   <e, b, e>8 <e, b,>16 <gis b e'>16 r16
   e, g, d
   \set TabStaff.minimumFret = 3
   \set TabStaff.restrainOpenStrings = ##t
   g,( a,) a, a, a,( g,)
   
   \set TabStaff.minimumFret = 0
   \set TabStaff.restrainOpenStrings = ##f
   e, g
}

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \tempo 4 = 96
  \repeat volta 2 { \bar ".|:"
    e1 e e b \break
    e a e2 b2 e1
  }
}

\markup \vspace #1

