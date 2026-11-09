// Package tab parses the ".song" file format's "score:" field — a
// LilyPond-*compatible* source file, in a clean, intentionally bounded
// subset — into real note-level tab data for rendering, reusing this
// app's existing chart renderers (internal/pdfnative for PDF, the web
// chart's HTML/CSS for the browser).
//
// Scope, grounded in the real legacy LaTeX+LilyPond books (this repo's
// Mercy/, MySongs/, "Other Lies/", Trio/ directories, pre-dating the
// .song format): those riffs are always written as a \new TabStaff
// block of \relative-pitch music — a single melodic line or small
// chords, the kind of thing printed as a few bars of tab under a song's
// title or before a chorus. There is no fret/string number written
// explicitly anywhere in these files: LilyPond computes string+fret
// itself from the written pitch, the TabStaff's string tuning, and a
// \set TabStaff.minimumFret preference — this package reproduces that,
// not just the notation. A sibling \new Staff/Voice carrying the same
// pitches (for engraved standard notation) is recognized but not
// rendered yet — see ParseScore's doc comment.
//
// \chordmode content is intentionally NOT supported — chord charts are
// the .song format's own "grid:" syntax's job; a "score:" file is only
// ever the note-level riff grid: can't express.
package tab

// Riff is one score's worth of note-level tab data: a sequence of bars,
// each a sequence of beats, each beat zero or more simultaneous notes
// (a chord) or a rest.
type Riff struct {
	Tuning      []int // open-string MIDI pitches, index 0 = string 1 (highest, matching this package's and LilyPond's own convention). Unused (nil) when Drums is true — a drum kit has no string tuning.
	MinimumFret int   // the TabStaff.minimumFret in effect at the START of the riff; see Beat.MinimumFret for mid-riff changes. Unused when Drums is true.

	// Drums marks a riff parsed from a "\new DrumStaff { \drummode {...} }"
	// block (see parseDrumStaffBody) rather than a "\new TabStaff"
	// fretted-note one. A drum Beat's Notes carry a General MIDI
	// percussion number in MIDI (String/Fret are unused, always 0) —
	// see drumPitchNames and ToAlphaTexDrums.
	Drums bool

	Bars []Bar
}

// Bar is one inferred bar's worth of beats — see inferBars' doc comment
// for how bar boundaries are found (LilyPond's own files never write an
// explicit "|" in this content, so neither do we; both infer it from
// cumulative duration against the time signature).
type Bar struct {
	Beats []Beat
}

// Beat is one rhythmic event: a rest, or 1+ simultaneous fretted notes.
type Beat struct {
	Rest     bool
	Duration string // a LilyPond duration token, e.g. "8", "4.", "16" — kept as-is; see durationBeats for its numeric meaning
	Notes    []Note

	// MinimumFret, when > 0, is a \set TabStaff.minimumFret value that
	// took effect starting at this beat (0 means "unchanged from the
	// riff's or the previous beat's value") — mercy.ly's riff, for
	// example, raises it partway through a phrase to keep a later chord
	// higher up the neck instead of picking the (also valid) open-string
	// voicing LilyPond would otherwise prefer.
	MinimumFret int

	// HammerPull marks a LilyPond slur "(" opening on this beat's note
	// (real example: Mercy/always-free/verse.ly's "g,( a,)",
	// Mercy/bad-spell/intro.ly's "bes16( c'4)") — on a guitar TabStaff a
	// slur between two fretted notes is how a hammer-on (ascending
	// pitch) or pull-off (descending pitch) is written; which one it is
	// isn't stored here (it's derived from comparing this beat's and the
	// next sounding beat's pitch — see the renderer). Sufficient for
	// every real slur in this app's own corpus, all exactly two notes;
	// a LilyPond slur spanning 3+ notes would need matching open/close
	// tracking this field doesn't do.
	HammerPull bool

	// HasSlideFrom/SlideFromFret: a "\grace <note>( \glissando <main
	// note>)" preceding this beat (real example: Mercy/whiskey-beer-and-
	// wine/intro.ly's "f~ f8 ees8~ \grace f16( \glissando g8)") — a
	// slide INTO this beat's note from SlideFromFret on the SAME string
	// (a slide, by definition, never changes string). The grace note's
	// own duration is real LilyPond "borrowed time" with no separate
	// rhythmic value of its own, which this subset doesn't model as a
	// distinct event — only the slide-from fret survives, attached to
	// the note it slides into.
	HasSlideFrom  bool
	SlideFromFret int

	// BendSemitones/PalmMute: this subset's OWN extension, not grounded
	// in this app's real legacy corpus (neither appears there) — added
	// ahead of a planned Guitar Pro importer, which carries a per-note
	// bend amount and palm-mute flag natively, and needs SOME text
	// representation to serialize into this package's LilyPond-subset
	// source. Uses real, valid LilyPond syntax rather than an invented
	// token: a note's own text articulation, "note^\"label\"" (LilyPond's
	// genuine way to attach free text above a note — see ParseScore's
	// grammar doc comment for the recognized label vocabulary). Zero/
	// false means "no bend"/"not muted".
	//
	// BendSemitones is in semitones (not quarter-tones, not whole
	// "steps") to match this package's own MIDI-pitch convention
	// elsewhere: "full" = 2 (a whole step), "1/2" = 1, "1/4" = 0.5.
	BendSemitones float64
	BendRelease   bool // "full (release)" — bends back down to the written pitch before the beat ends
	PalmMute      bool
}

// Note is one fretted note within a Beat.
type Note struct {
	MIDI   int
	String int // 1-based, resolved against the Riff's Tuning
	Fret   int
	Tied   bool // this note continues the previous beat's same pitch (a "~" tie) rather than being freshly struck
}
