\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

<<

\chords { e2:m b a g }

\new TabStaff {
   \tabFullNotation
   \tempo 4 = 90
   \set TabStaff.minimumFret = 5
   \set TabStaff.restrainOpenStrings = ##t
   e8 g
   \set TabStaff.restrainOpenStrings = ##f
   g b
   
   \set TabStaff.minimumFret = 5
   \set TabStaff.restrainOpenStrings = ##t
   b, d
   \set TabStaff.restrainOpenStrings = ##f
   d g
   
   \set TabStaff.minimumFret = 3
   \set TabStaff.restrainOpenStrings = ##t
   a, cis
   \set TabStaff.restrainOpenStrings = ##f
   d g

   \set TabStaff.minimumFret = 2
   \set TabStaff.restrainOpenStrings = ##t
   g, b,
   \set TabStaff.restrainOpenStrings = ##f
   d g
}

>>