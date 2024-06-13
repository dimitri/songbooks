\include "header.ly"

\repeat volta 3 {
  \alternative {
    \volta 1,2 {
    <<
     \chords  { a1:sus2 a:sus2 fis:m g }
     
     \new TabStaff {
        \tabFullNotation
        \set Timing.beamExceptions = #'()
        \set TabStaff.minimumFret = 5
        \set TabStaff.restrainOpenStrings = ##t
        a8 cis' g' a cis' g' cis' fis'
        cis'8 fis' cis' e'4 e'8 cis' a
        \set TabStaff.minimumFret = 2
        fis8 a e' a fis' e' a e'
        \set TabStaff.minimumFret = 4
        g8 b e' b fis' e' b e'   
     }
   >>
   }
   \volta 3 {
   <<
     \chords  { a1:sus2 a:sus2 fis:m g }
     
     \new TabStaff {
        \tabFullNotation
        \set Timing.beamExceptions = #'()

        \set TabStaff.minimumFret = 5
        \set TabStaff.restrainOpenStrings = ##t

        cis'8 e' b' cis' e' b' e' cis''
        e'8 b' e' a' e' b'4.

        a8 cis' fis' cis' g' fis' cis' fis'

        \set TabStaff.minimumFret = 4
        gis8 b e' b fis' e' b e'
     }
   >>
   }
  }
}
