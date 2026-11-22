package pdfnative

// This file holds the PDF-export request/options types that used to live
// in internal/pdf, back when a Server could choose between pdfnative
// (pure Go) and internal/pdf.Bridge (shelled out to the retired v2/ Typst
// pipeline). That choice is gone — pdfnative is the only PDF renderer now
// — so these types moved here rather than staying in a now-single-purpose
// internal/pdf package.

// Mode selects which chart view a PDF export renders.
type Mode string

const (
	ModeDefault Mode = ""       // chords + lyrics — every existing caller's implicit mode today
	ModeJazz    Mode = "jazz"   // chords only, denser — the print analogue of the web Jazz chart view
	ModeMap     Mode = "map"    // section order only, no chords, no lyrics — a one-page "roadmap"
	ModeLyrics  Mode = "lyrics" // lyrics + section labels, no chord chart — for the singer
)

// ModeFromString maps a request's raw ?mode= value to a Mode, falling back
// to ModeDefault for anything unrecognized (including "") rather than
// erroring — an export link with a typo'd or stale mode param should still
// produce a PDF, just the default one, not a 400.
func ModeFromString(s string) Mode {
	switch Mode(s) {
	case ModeJazz, ModeMap, ModeLyrics:
		return Mode(s)
	default:
		return ModeDefault
	}
}

// Options controls the page target/theme and view-mode/notes choices
// pdfnative honors.
type Options struct {
	Theme string // "light" or "dark"; empty defaults to "light"
	Mode   Mode   // zero value ModeDefault — unchanged behavior for every existing call site

	// HideNotes, when true, omits each section's free-text performance/
	// arrangement Notes (song.Section.Notes) from the output. The zero
	// value (false) matches pdfnative's existing unconditional rendering
	// of them, so every Options{} call site written before this field
	// existed keeps showing notes exactly as it always has — the
	// "no-regression default" is deliberately the inverse-sense name
	// (HideNotes, not WithNotes/ShowNotes) specifically so the zero value
	// is safe.
	HideNotes bool

	// Columns overrides the chart's column count (normally 2: sections
	// flow down two newspaper-style columns, like the web Song view on a
	// wide screen). 1 forces a single full-width column (?cols=1 on an
	// export link). Zero means "use the renderer's normal default". A
	// Song-layout chart with no lyrics anywhere prints in one column
	// regardless — see songCols.
	Columns int
}

// Cols returns the effective column count: Columns if set, else 2.
func (o Options) Cols() int {
	if o.Columns > 0 {
		return o.Columns
	}
	return 2
}

// SongRef is one resolved setlist entry, ready to materialize.
type SongRef struct {
	Group string // the containing Set's label, e.g. "Set 1" — "" groups as "Songs"
	Book  string
	Stem  string
	Body  string
	Note  string // the setlist entry's own gig note (setlist.Entry.Note), e.g. "capo 2, watch the bridge tempo" — printed in the contents/running-order table, not part of the song's own chart
}

// FrontMatter controls the single cover page rendered before the
// contents/setlist table. Its title is CoverTitle, else EventTitle, else
// the export's own title; BandName/Date/Venue print beneath it when set.
type FrontMatter struct {
	CoverTitle string
	EventTitle string
	BandName   string
	Date       string
	Venue      string
}
