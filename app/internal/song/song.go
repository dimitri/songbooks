// Package song is a Go port of the .song grammar parsed by v2/lib/song.typ.
//
// Unlike the Typst parser, this package never interprets or transposes chord
// text: it only pulls apart the structural syntax (bars, sections, lyric
// brackets) it needs to drive an editor and viewer, and keeps every token's
// text byte-for-byte as written. That keeps round-tripping safe for content
// this app doesn't understand yet (duration tags, score references).
package song

// MetaKeys are the header fields recognized by v2/lib/song.typ's META-KEYS,
// plus "reference" (a companion field this app adds; v2/lib/song.typ must
// also allow it for the key to survive a Typst-side read).
var MetaKeys = []string{
	"title", "artist", "key", "tempo", "time", "capo",
	"transpose", "map", "idiom", "flats", "reference",
	"cover", "cover-credit",
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
	Sections []Section
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

// Section is one [Label] or [Label] (repeat) block.
//
// A (repeat) section carries no measures/lyrics of its own, mirroring
// v2/lib/song.typ (the Typst renderer prints just a label marker for it).
type Section struct {
	Label  string
	Repeat bool

	Time  string // per-section "time:" override, "" if absent
	Score string // raw "score:" filename, "" if absent; not otherwise interpreted

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
	Open   bool   // this bar opens a "|: ... :|" repeat
	Close  bool   // this bar closes a "|: ... :|" repeat
	Volta  string // raw volta marker, e.g. "1." or "1,2,3.", "" if absent
	Meter  string // raw per-bar meter override, e.g. "2/4", "" if absent
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
