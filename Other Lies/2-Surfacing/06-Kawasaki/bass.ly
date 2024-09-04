\include "header.ly"

\new TabStaff \with { stringTunings = #bass-tuning } {  
  \tabFullNotation
  \set Timing.beamExceptions = #'()
  \set TabStaff.minimumFret = 7
  \set TabStaff.restrainOpenStrings = ##t
  
  \tempo 4 = 90
  
  \relative {
    fis,8 e16 fis16 r16 e16 fis a b8 d8 e4
  }
}
