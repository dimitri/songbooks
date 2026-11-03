// Package chordsym renders a chord symbol the way a real chart does —
// Brandt & Roemer house style, the same rules v2/lib/chord.typ encodes for
// the Typst pipeline, ported to HTML/CSS: bold root, small raised quality
// text, a triangle for a major seventh instead of literal "maj7", a ø for
// half-diminished and ° for diminished instead of the ASCII people actually
// type (m7b5, dim7 — ø and ° aren't easy to type), the minor marker dropped
// below the baseline, real flat/sharp glyphs instead of ASCII, and a
// smaller bass note after a slash.
//
// Liberal in what it accepts, strict in what it shows: any of the common
// ways a human or a tab site writes a quality normalize to the same
// standard glyph, so the source file can stay in whatever form was easiest
// to type.
package chordsym

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

const (
	triangle   = "△" // major 7th
	halfDimSym = "ø" // half-diminished (written m7b5, mi7b5, -7b5, ø7... — hard to type ø directly)
	dimSym     = "°" // diminished (written dim, dim7, o, o7...)
	flatGlyph  = "♭"
	sharpGlyph = "♯"
)

// Order matters: half-diminished must be checked before the plain-minor
// prefix (both start with "m"/"mi"/"min"/"-"), and before "dim" is checked
// generically (half-dim is never spelled "dim").
var (
	// "m7b5", "mi7b5", "min7b5", "-7b5", "m9b5", "m7-5", already-correct "ø"/"ø7"...
	halfDimRE        = regexp.MustCompile(`(?i)^(?:mi|min|m|-)(\d*)[b-]5`)
	halfDimLiteralRE = regexp.MustCompile(`^ø(\d*)`)
	// "dim7", "dim", "o7", "o", already-correct "°"/"°7"
	dimRE        = regexp.MustCompile(`(?i)^dim(\d*)`)
	dimOLetterRE = regexp.MustCompile(`^o(\d*)`)
	dimLiteralRE = regexp.MustCompile(`^°(\d*)`)
	// "aug5", "aug7", "+5", already-correct "+"
	augRE = regexp.MustCompile(`(?i)^aug(\d*)`)

	minorRE = regexp.MustCompile(`^(m|min|mi|-)`)
)

// Parts is a chord symbol split into root/quality/bass, mirroring
// v2/lib/chord.typ's parse(). "N.C." and "%" (and anything else with no
// recognizable root) come back with Root == "" and the whole symbol in
// Quality, rendered as plain italic text.
type Parts struct {
	Root    string
	Quality string
	Bass    string
}

// Split mirrors chord.typ's split-root/parse: a root is one letter plus an
// optional b/# accidental; everything after is quality; an optional "/bass"
// suffix is split the same way.
func Split(sym string) Parts {
	s := strings.TrimSpace(sym)
	if s == "" || s == "N.C." || s == "%" {
		return Parts{Quality: s}
	}
	root, rest, ok := splitRoot(s)
	if !ok {
		return Parts{Quality: s}
	}
	quality := rest
	bass := ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		quality = rest[:i]
		if b, _, ok := splitRoot(rest[i+1:]); ok {
			bass = b
		}
	}
	return Parts{Root: root, Quality: quality, Bass: bass}
}

func splitRoot(s string) (root, rest string, ok bool) {
	if s == "" {
		return "", "", false
	}
	c := s[0]
	if !(c >= 'A' && c <= 'G') {
		return "", "", false
	}
	n := 1
	if len(s) > 1 && (s[1] == 'b' || s[1] == '#') {
		n = 2
	}
	return s[:n], s[n:], true
}

// pitchClassOf maps a natural letter to its chromatic pitch class (C=0).
var pitchClassOf = map[byte]int{'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11}

// PitchClass returns root's chromatic pitch class 0-11 (C=0), or -1 for an
// empty/unrecognized root. Exposed so the view can emit it as a data
// attribute (data-pc) on each rendered root/bass note — live transpose
// (song.js) only needs to shift this number and re-spell it; it never has
// to re-parse chord text client-side.
func PitchClass(root string) int {
	if root == "" {
		return -1
	}
	pc, ok := pitchClassOf[root[0]]
	if !ok {
		return -1
	}
	if len(root) > 1 {
		switch root[1] {
		case '#':
			pc++
		case 'b':
			pc--
		}
	}
	return ((pc % 12) + 12) % 12
}

func glyphs(s string) string {
	s = strings.ReplaceAll(s, "b", flatGlyph)
	s = strings.ReplaceAll(s, "#", sharpGlyph)
	return s
}

func esc(s string) string { return html.EscapeString(glyphs(s)) }

// Render produces the chart-style HTML for one chord symbol:
// <span class="csym">...</span>, safe to embed directly (all dynamic text
// is HTML-escaped).
// Marker identifies which special symbol (if any) a chord quality starts
// with — the same classification Render (HTML/web) applies, exposed
// separately so another renderer (e.g. internal/pdfnative, which can't
// just emit HTML/CSS spans) can reuse the exact same rules instead of
// re-deriving its own regex set.
type Marker int

const (
	MarkerNone Marker = iota
	MarkerMajor7
	MarkerHalfDim
	MarkerDim
	MarkerAug
	MarkerMinor
)

// QualityParts is a chord quality (the text after the root, before any
// slash-bass — see Split) decomposed into its leading marker and
// whatever extension text remains after it.
type QualityParts struct {
	Marker    Marker
	MarkerRaw string // MarkerMinor only: the raw text actually typed ("m", "mi", "min", "-")
	Ext       string
}

// DecomposeQuality applies the same half-diminished/diminished/augmented/
// major-seventh/minor classification Render uses, without committing to
// HTML output — the shared logic behind both Render and any other
// renderer's own chord-symbol drawing.
func DecomposeQuality(q string) QualityParts {
	if strings.HasPrefix(q, "maj") {
		return QualityParts{Marker: MarkerMajor7, Ext: q[len("maj"):]}
	}
	var rest string
	if matchRest(halfDimRE, q, &rest) || matchRest(halfDimLiteralRE, q, &rest) {
		return QualityParts{Marker: MarkerHalfDim, Ext: rest}
	}
	if matchRest(dimRE, q, &rest) || matchRest(dimOLetterRE, q, &rest) || matchRest(dimLiteralRE, q, &rest) {
		return QualityParts{Marker: MarkerDim, Ext: rest}
	}
	if matchRest(augRE, q, &rest) {
		return QualityParts{Marker: MarkerAug, Ext: rest}
	}
	if m := minorRE.FindString(q); m != "" {
		return QualityParts{Marker: MarkerMinor, MarkerRaw: m, Ext: q[len(m):]}
	}
	return QualityParts{Ext: q}
}

func Render(sym string) string {
	p := Split(sym)
	if p.Root == "" {
		if p.Quality == "" {
			return ""
		}
		return `<span class="csym csym-plain">` + esc(p.Quality) + `</span>`
	}

	var b strings.Builder
	b.WriteString(`<span class="csym">`)
	b.WriteString(`<span class="csym-root" data-pc="` + strconv.Itoa(PitchClass(p.Root)) + `">` + esc(p.Root) + `</span>`)

	dq := DecomposeQuality(p.Quality)
	switch dq.Marker {
	case MarkerMajor7:
		b.WriteString(`<span class="csym-triangle">` + triangle + `</span>`)
	case MarkerHalfDim:
		b.WriteString(`<span class="csym-dim">` + halfDimSym + `</span>`)
	case MarkerDim:
		b.WriteString(`<span class="csym-dim">` + dimSym + `</span>`)
	case MarkerAug:
		b.WriteString(`<span class="csym-dim">+</span>`)
	case MarkerMinor:
		b.WriteString(`<span class="csym-minor">` + esc(dq.MarkerRaw) + `</span>`)
	}
	writeExt(&b, dq.Ext)

	if p.Bass != "" {
		b.WriteString(`<span class="csym-bass">/<span data-pc="` + strconv.Itoa(PitchClass(p.Bass)) + `">` + esc(p.Bass) + `</span></span>`)
	}
	b.WriteString(`</span>`)
	return b.String()
}

// DecomposeQualityJazz is DecomposeQuality's iReal Pro counterpart —
// verified against real iReal Pro PDF exports (not memory, not a
// third-party fakebook site): minor is always written "-", and critically
// a half-diminished chord is NOT substituted with the ø glyph the way
// Render does for every other mode — iReal Pro spells it out literally,
// e.g. "B-7♭5", which is exactly what you get by treating "m7b5" as plain
// minor ("-") with extension "7b5" and skipping the half-dim check
// entirely. Dim/aug keep the same °/+ treatment as the shared decomposer
// (not contradicted by any of the four real exports checked).
func DecomposeQualityJazz(q string) QualityParts {
	if strings.HasPrefix(q, "maj") {
		return QualityParts{Marker: MarkerMajor7, Ext: q[len("maj"):]}
	}
	if m := minorRE.FindString(q); m != "" {
		return QualityParts{Marker: MarkerMinor, MarkerRaw: "-", Ext: q[len(m):]}
	}
	var rest string
	if matchRest(dimRE, q, &rest) || matchRest(dimOLetterRE, q, &rest) || matchRest(dimLiteralRE, q, &rest) {
		return QualityParts{Marker: MarkerDim, Ext: rest}
	}
	if matchRest(augRE, q, &rest) {
		return QualityParts{Marker: MarkerAug, Ext: rest}
	}
	return QualityParts{Ext: q}
}

// RenderJazz is Render's iReal Pro counterpart, used only by jazz chart
// mode (see the "chordJazz" template func, internal/webassets/embed.go).
// Same root/bass handling (bass note still a <span class="csym-bass">
// child — jazz mode repositions it below-right via CSS, see app.css —
// stacked rather than inline, matching the real exports), but quality
// goes through DecomposeQualityJazz instead of DecomposeQuality.
func RenderJazz(sym string) string {
	p := Split(sym)
	if p.Root == "" {
		if p.Quality == "" {
			return ""
		}
		return `<span class="csym csym-plain">` + esc(p.Quality) + `</span>`
	}

	var b strings.Builder
	b.WriteString(`<span class="csym">`)
	b.WriteString(`<span class="csym-root" data-pc="` + strconv.Itoa(PitchClass(p.Root)) + `">` + esc(p.Root) + `</span>`)

	dq := DecomposeQualityJazz(p.Quality)
	switch dq.Marker {
	case MarkerMajor7:
		b.WriteString(`<span class="csym-triangle">` + triangle + `</span>`)
	case MarkerDim:
		b.WriteString(`<span class="csym-dim">` + dimSym + `</span>`)
	case MarkerAug:
		b.WriteString(`<span class="csym-dim">+</span>`)
	case MarkerMinor:
		b.WriteString(`<span class="csym-minor">` + esc(dq.MarkerRaw) + `</span>`)
	}
	writeExt(&b, dq.Ext)

	if p.Bass != "" {
		b.WriteString(`<span class="csym-bass">/<span data-pc="` + strconv.Itoa(PitchClass(p.Bass)) + `">` + esc(p.Bass) + `</span></span>`)
	}
	b.WriteString(`</span>`)
	return b.String()
}

// matchRest tries re against q; on a match it replaces *rest with whatever
// the match's capture group(s) left over (so e.g. "m9b5" -> quality kind
// half-dim, with "9" surviving as the extension still to render) and
// returns true. On no match, *rest is left untouched and it returns false.
func matchRest(re *regexp.Regexp, q string, rest *string) bool {
	loc := re.FindStringSubmatchIndex(q)
	if loc == nil {
		return false
	}
	// Reassemble: captured digits (if any) immediately followed by
	// whatever text came after the whole match (rare, e.g. "m7b5#9").
	var digits string
	if loc[2] >= 0 {
		digits = q[loc[2]:loc[3]]
	}
	*rest = digits + q[loc[1]:]
	return true
}

func writeExt(b *strings.Builder, q string) {
	if q != "" {
		b.WriteString(`<span class="csym-ext">` + esc(q) + `</span>`)
	}
}
