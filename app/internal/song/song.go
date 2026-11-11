// Package song parses and serializes the ".song" file format — the
// app's one text representation for a song's header, structure, chord
// grid and lyrics.
//
// This package never interprets or transposes chord text: it only pulls
// apart the structural syntax (bars, sections, lyric brackets, score
// voices) it needs to drive an editor and viewer, and keeps every
// token's text byte-for-byte as written. That keeps round-tripping safe
// for content this package doesn't itself understand (duration tags, a
// score voice's own LilyPond-subset source — see ScoreVoice's own doc
// comment, and internal/tab.ParseVoice, where that one actually gets
// resolved).
package song

// MetaKeys are the header fields recognized by v2/lib/song.typ's META-KEYS,
// plus "reference" (a companion field this app adds; v2/lib/song.typ must
// also allow it for the key to survive a Typst-side read).
var MetaKeys = []string{
	"title", "artist", "key", "tempo", "time", "capo",
	"transpose", "map", "idiom", "flats", "reference",
	"cover", "cover-credit", "swing",
}

func isMetaKey(k string) bool {
	for _, mk := range MetaKeys {
		if mk == k {
			return true
		}
	}
	return false
}

// MetaField is one recognized header line, in source order.
type MetaField struct {
	Key   string
	Value string
}

// Song is the whole parsed .song file.
type Song struct {
	Meta     []MetaField
	Chords   []ChordRef
	Sections []Section
}

// ChordRef is one "chord: Name Spec" header line — a fretboard-diagram
// reference for the chart's legend row (the small grid-with-dots boxes
// printed under a song's title). Name is a plain chordsym symbol, same
// grammar as every chord already written in grid:/lyrics ("Cm", "Ab",
// "D9/F#") — not a separate naming scheme. Spec is kept raw here, same
// convention as a ScoreVoice's own Body: this package only pulls apart the file's
// structural syntax, never interprets it — see internal/chorddiagram for
// the actual grid grammar (fret position + per-string X/O/digit/barre)
// and the real legacy source it's compatible with.
type ChordRef struct {
	Name string
	Spec string
}

// Get returns the value of the first occurrence of key, or "" if absent.
func (s *Song) Get(key string) string {
	for _, f := range s.Meta {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

// Set inserts or overwrites key, preserving its original position if present.
func (s *Song) Set(key, value string) {
	for i, f := range s.Meta {
		if f.Key == key {
			s.Meta[i].Value = value
			return
		}
	}
	s.Meta = append(s.Meta, MetaField{Key: key, Value: value})
}

// ScoreVoice is one "score[Label]: <content>" block — Label is this
// voice's name (freeform; "drum"/"drums" and "bass" are the two this
// package's parser (internal/tab.ParseVoice) recognizes specially, to
// skip the "\new DrumStaff { \drummode {...} } }"/"\new TabStaff \with
// {...} { \relative {...} }" boilerplate a bare drum or bass voice would
// otherwise need — anything else defaults to guitar tuning), Body is the
// LilyPond-subset source verbatim, never interpreted by this package
// (see this package's own doc comment).
type ScoreVoice struct {
	Label string
	Body  string
}

// Section is one [Label] or [Label] (repeat) block.
//
// A (repeat) section carries no measures/lyrics of its own, mirroring
// v2/lib/song.typ (the Typst renderer prints just a label marker for it).
type Section struct {
	Label  string
	Repeat bool

	Time string // per-section "time:" override, "" if absent

	// Scores are this section's "score[label]: ..." voices — real,
	// bounded-subset LilyPond source, inline in the file (single
	// source of truth: no separate database row, no out-of-band
	// lookup — a .song file is meant to be the whole song, readable,
	// editable and git-diffable on its own, including its notation; see
	// this package's own doc comment and internal/tab.ParseVoice, which
	// resolves each one's Body into a Riff). One entry per "score[...]:"
	// line, in source order; a section with none has Scores == nil.
	Scores []ScoreVoice

	// Times is the section's "times:" line — how many passes through its
	// grid it actually takes (its own, when it has one, or an inherited
	// one for a bare section — see annotateStructure in httpapi). 0 means
	// absent, same as 1 (a single pass); only a value > 1 changes
	// anything. This is a web-viewer-only field: v2/lib/song.typ's Typst
	// parser doesn't act on it (it silently ignores an unrecognized
	// "times:" line rather than printing it as a lyric — see that file),
	// so a PDF built through the Typst pipeline still shows the section's
	// grid exactly as written, same as before this field existed.
	Times int

	// BarsPerLine mirrors the "bars:" line: nil if absent (default of 4
	// measures/line applies), one element for a uniform override ("bars: 6"),
	// or several for an explicit per-row list ("bars: 4,5").
	BarsPerLine []int

	Measures []Measure
	Lyrics   []LyricLine
	// Notes are free-text performance/arrangement directions for this
	// section occurrence — "GTR + claviers only", "Entree Basse (ronde)",
	// the kind of inline annotation a real iReal Pro chart carries. Each
	// "note:" line in the source is its own entry, in source order.
	// Attached to the SECTION OCCURRENCE, not to an individual bar-row:
	// when a direction is really about one specific pass through a
	// repeated phrase, write that pass as its own [Section] entry (this
	// app's structure already supports that — see how Verse/Chorus
	// repeats are commonly written as separate bare entries) and put the
	// note on that occurrence.
	Notes []string
}

// Measure is one |...| bar. Tokens are kept verbatim (chord symbols, "%",
// ".", "R"/"r", or anything else a chart writes) — never transposed, never
// stripped of a ":duration" suffix.
type Measure struct {
	Tokens []string
	Open   bool // this bar opens a "|: ... :|" repeat
	Close  bool // this bar closes a "|: ... :|" repeat
	// RepeatCount is how many times a "|: ... :|" bracket is actually
	// played — only meaningful (and only ever set) on the Open measure.
	// 0 means "not stated" — the standard, implicit "play it twice"
	// every plain "|: ... :|" bracket already means, unchanged. A
	// stated count is written right after the opening colon with no
	// space, e.g. "|:4 F#m | D | Bm | E :|" for a 4-bar phrase played
	// four times through — the bracket-level counterpart to a whole
	// SECTION's own "times:" line (see Section.Times's doc comment):
	// that one multiplies an inherited/own section length, this one
	// states literally how many times the written bars themselves
	// repeat, which a bare "|: :|" can't express beyond the implicit 2.
	RepeatCount int
	Volta       string // raw volta marker, e.g. "1." or "1,2,3.", "" if absent
	Meter       string // raw per-bar meter override, e.g. "2/4", "" if absent
}

// LyricLine is one chorded lyric line: "Some [Gm]words here".
type LyricLine struct {
	Segments []LyricSeg
}

// LyricSeg is a (chord, text) run; Chord is only meaningful when HasChord.
type LyricSeg struct {
	HasChord bool
	Chord    string
	Text     string
}

// TokenChord strips a ":duration" tag ("Gm:4.", "F:8") off a raw grid
// token down to the bare chord/rest symbol a renderer should actually
// show. Measure.Tokens are kept verbatim by design (see this type's own
// doc comment above) — nothing in this app interprets the tag for layout
// today, so showing it raw is a display bug, not a meaningful omission,
// in every renderer that turns a token into on-page text (the native PDF
// chart and the web Play view's beat-slot chord labels both hit this).
// Only the FIRST ':' matters: a bass-note slash chord ("C/G") has no
// colon, so this never touches a real chord symbol.
func TokenChord(tok string) string {
	for i := 0; i < len(tok); i++ {
		if tok[i] == ':' {
			return tok[:i]
		}
	}
	return tok
}
