\include "header.ly"

\new ChordNames \with {
  \override BarLine.bar-extent = #'(-2 . 2)
  \consists "Bar_engraver"
}

\chordmode {
  \set noChordSymbol = \markup { \typewriter "2/4" }
  \transpose c d {
    { \bar "|" f1 aes f aes r }
  }
}
