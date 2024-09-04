\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\repeat volta 2 {
  <<
    \chords  { fis2:m a2 fis1:m }
    
    \new TabStaff {
       \set Timing.beamExceptions = #'()
       \set TabStaff.minimumFret = 9
       \set TabStaff.restrainOpenStrings = ##t
  
       <a' cis'' fis''>2
       <a' cis'' e''>2
       <fis' a' cis''>1
    }
  >>
}
