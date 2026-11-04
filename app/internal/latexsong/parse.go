// Package latexsong imports and exports the legacy LaTeX "songs" package
// format (\usepackage[chorded,noscripture]{songs}) this repo's older
// song trees (MySongs/, Trio/, Mercy/, "Other Lies"/) were originally
// authored in, converting to and from this app's own internal/song model.
//
// Scope and conventions are taken from a full survey of the real corpus
// (every .tex file under those four trees plus songs.sty itself), not
// guessed from the package docs alone — see each function's own comment
// for the specific LaTeX construct it handles and why.
package latexsong

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"app/internal/song"

	"app/internal/lilychord"
)

// ParsedSong is one \beginsong...\endsong block, converted.
type ParsedSong struct {
	Title     string
	Artist    string
	Capo      string // raw \capo{N} value, "" if absent
	Transpose string // raw \transpose{N} value, "" if absent — see Parse's own comment on why this is never applied to chord text
	Song      *song.Song
}

var (
	beginsongRE = regexp.MustCompile(`\\beginsong\{([^}]*)\}(?:\s*\[\s*by\s*=\s*\{([^}]*)\}\s*\])?`)
	textnoteRE  = regexp.MustCompile(`\\textnote\{([^}]*)\}`)
	echoRE      = regexp.MustCompile(`\\echo\{([^}]*)\}`)
	capoRE      = regexp.MustCompile(`\\capo\{([^}]*)\}`)
	transposeRE = regexp.MustCompile(`\\transpose\{([^}]*)\}`)
	chordRE     = regexp.MustCompile(`\\\[([^\]]*)\]`)
	nolyricsRE  = regexp.MustCompile(`^\{\\nolyrics(.*)\}$`)
	lilypondRE  = regexp.MustCompile(`\\lilypondfile(?:\[[^\]]*\])?\{[^}]*\}`)
	caretRE     = regexp.MustCompile(`\^`)
	// lilypondStandaloneRE matches a line that is NOTHING BUT a
	// \lilypondfile reference (the real corpus's own actual shape,
	// confirmed by survey — a mid-line occurrence mixed with other
	// content is left to lilypondRE's plain strip, unchanged) —
	// captures the referenced filename so it can be resolved to real
	// chord data for a song whose chords live ONLY in that .ly file
	// (see ParseWithResolver).
	lilypondStandaloneRE = regexp.MustCompile(`^\\lilypondfile(?:\[[^\]]*\])?\{([^}]*)\}$`)
)

// unescapeLatex undoes the one escape this corpus's titles/artists
// actually use ("\&" for a literal "&" — e.g. "Whiskey, Beer \& Wine",
// "Simon \& Garfunkel"). Curly and straight apostrophes are left as-is;
// both appear in the corpus and neither needs normalizing for this
// app's own grammar, which has no apostrophe-escaping of its own.
func unescapeLatex(s string) string {
	return strings.ReplaceAll(s, `\&`, "&")
}

// normalizeChord trims stray whitespace inside the brackets (a real typo
// found in the corpus, "\[Em ]baby") and accepts both flat spellings the
// corpus uses interchangeably — lowercase "b" (the common, de-facto
// standard) and the songs package's own native "&" glyph (e.g. "A&",
// "B&7") — normalizing to "b" so this app's chordsym package, which only
// ever sees "b", parses it the same way regardless of source spelling.
// Everything else (quality suffixes, slash bass notes, the package's
// sus2/sus4 guitarist shorthand like "Bb2"/"D4") passes through
// unchanged and opaque, same policy internal/importer's UG parser uses.
func normalizeChord(raw string) string {
	c := strings.TrimSpace(raw)
	return strings.ReplaceAll(c, "&", "b")
}

// buildGrid collapses an ordered chord-token list into this app's own
// Measure sequence, one bar per entry, collapsing an immediate repeat to
// "%" — identical convention to internal/importer's UGSection.Grid(),
// and for the same reason: plain tab/lead-sheet text essentially never
// states how many beats each chord actually holds, so "one bar per
// chord change" is the only honest default; real bar placement is a
// correction the editor's own bar-grid builder is for.
func buildGrid(chords []string) []song.Measure {
	var out []song.Measure
	for i, c := range chords {
		if i > 0 && c == chords[i-1] {
			out = append(out, song.Measure{Tokens: []string{"%"}})
			continue
		}
		out = append(out, song.Measure{Tokens: []string{c}})
	}
	return out
}

// sectionBuilder accumulates one \beginverse/\beginchorus (or standalone
// \textnote) block's content as it's scanned.
type sectionBuilder struct {
	label     string
	open      bool
	lines     []song.LyricLine
	chords    []string // every chord seen in this section, attached or bare, in order
	hadChords bool      // at least one \[Chord] actually seen (vs. a chordless lyrics-only section)
	// lyMeasures is this section's grid as resolved from a sibling .ly
	// file's \chordmode block (see ParseWithResolver) — used only when
	// chords is empty, i.e. this section's chords were never written as
	// \[Chord] text at all, only as LilyPond notation.
	lyMeasures []song.Measure
}

// labelFromFilename derives a section label from a .ly filename when no
// \textnote preceded a standalone \lilypondfile reference (e.g. Still Got
// The Blues' intro.ly, referenced before any \beginverse at all) —
// "intro.ly" -> "Intro", "bridge.ly" -> "Bridge".
func labelFromFilename(name string) string {
	stem := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	stem = strings.ReplaceAll(stem, "-", " ")
	stem = strings.ReplaceAll(stem, "_", " ")
	if stem == "" {
		return "Intro"
	}
	r := []rune(stem)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// processContentLine turns one lyric/chord line from inside a \beginverse
// or \beginchorus block into this app's own inline "[Chord]word" segments,
// handling every non-chord construct the corpus's content lines use:
//   - \echo{text} (backing-vocal echo) becomes plain "(text)" — this
//     app's grammar has no separate "voice" concept, so folding it into
//     the main lyric text as a parenthetical is the closest honest
//     representation, not a loss of anything the target format could
//     otherwise keep.
//   - a bare "^" (the package's own syllable-alignment placeholder, used
//     when a later verse reuses an earlier verse's chords without
//     re-annotating them) carries no information this app's model can
//     represent and is dropped outright.
//   - \lilypondfile[...]{...} (a rendered-notation reference, sometimes
//     appearing mid-verse, not just before it) is stripped; this app's
//     grid/lyric model has no equivalent to embed.
//
// chordsOn mirrors \chordsoff's one-way "stop transcribing chords for the
// rest of this song" switch (see Parse's own doc comment) — when false,
// no \[Chord] is extracted even if one is literally present (defensive;
// the corpus never actually does this, authors stop writing them too).
func processContentLine(line string, chordsOn bool) (lyric song.LyricLine, chords []string, hasText bool) {
	// Leading indentation is the .tex source's own formatting (every
	// real file in the corpus indents verse/chorus body lines 1-2
	// spaces for readability), never part of the lyric itself — strip
	// it before anything else so it never leaks into Segments[0].Text.
	line = strings.TrimLeft(line, " \t")
	line = echoRE.ReplaceAllString(line, "($1)")
	line = lilypondRE.ReplaceAllString(line, "")
	line = caretRE.ReplaceAllString(line, "")

	if !chordsOn {
		// \chordsoff only suppresses PRINTING — it's common for an
		// author not to bother deleting the now-inert \[Chord] markup
		// on lines typed after it (real example: Trio/every-breath-you-
		// take.tex, \chordsoff then three more chorded-looking lines
		// before \chordson turns it back on for the bridge). Left as
		// literal "\[G]" text, this is a visible bug, not a harmless
		// quirk — strip the markers, keeping only the words.
		text := strings.TrimSpace(chordRE.ReplaceAllString(line, ""))
		if text == "" {
			return song.LyricLine{}, nil, false
		}
		return song.LyricLine{Segments: []song.LyricSeg{{Text: text}}}, nil, true
	}

	matches := chordRE.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		text := strings.TrimSpace(line)
		if text == "" {
			return song.LyricLine{}, nil, false
		}
		return song.LyricLine{Segments: []song.LyricSeg{{Text: line}}}, nil, true
	}

	var segs []song.LyricSeg
	if matches[0][0] > 0 {
		segs = append(segs, song.LyricSeg{Text: line[:matches[0][0]]})
	}
	hasRealText := false
	for i, m := range matches {
		chord := normalizeChord(line[m[2]:m[3]])
		chords = append(chords, chord)
		textEnd := len(line)
		if i+1 < len(matches) {
			textEnd = matches[i+1][0]
		}
		text := line[m[1]:textEnd]
		if strings.TrimSpace(text) != "" {
			hasRealText = true
		}
		segs = append(segs, song.LyricSeg{HasChord: true, Chord: chord, Text: text})
	}
	if len(segs) > 0 && strings.TrimSpace(segs[0].Text) != "" {
		hasRealText = true
	}
	return song.LyricLine{Segments: segs}, chords, hasRealText
}

// Parse converts raw LaTeX "songs"-package source (one or more
// \beginsong...\endsong blocks) into ParsedSong values, one per block —
// a whole .tex file may contain more than one in principle, though the
// real corpus never does (always exactly one per file).
//
// Constructs handled, per the survey this package's design is based on:
//   - \beginsong{Title}[by={Artist}] — title/artist, with "\&" unescaped.
//   - \beginverse/\endverse, \beginchorus/\endchorus — sections, default
//     labelled "Verse"/"Chorus".
//   - \textnote{Label} — read as that label for the verse/chorus
//     immediately following it (Bridge, Interlude, Solo, Outro, etc. are
//     all written this way, never as their own LaTeX environment — the
//     package has none); a \textnote with no following verse/chorus
//     before the next \textnote or \endsong becomes its own standalone,
//     lyric-less, grid-less section (a bare instrumental marker).
//   - \[Chord]word — inline chord, this app's own syntax up to the
//     backslash.
//   - {\nolyrics \[C] \[G] ...} and a run of bare trailing \[Chord]
//     tokens with no following word (instrumental fills/turnarounds) —
//     folded into the section's grid, never into Lyrics (matching this
//     app's internal/importer fix for the same shape: a lyric line with
//     no real text, just chords, isn't written to Lyrics at all).
//   - \capo{N} / \transpose{N} — read as this app's own capo:/transpose:
//     header fields. \transpose is deliberately NOT applied to the
//     literal chord text (it only prints a "capo N" annotation; the
//     real pitch-shifting macro is \transpose, which the corpus's own
//     authors already keep written in UNSHIFTED chord names, same as
//     this app's transpose: header is defined to mean — see
//     Mercy/gone/gone.tex's \transpose{1} next to literally-written
//     \[Bm] chords that only SOUND like Cm).
//   - \chordsoff — a one-way switch (the corpus never uses the
//     package's \chordson counterpart): every chord after this point is
//     dropped from the source text entirely (plain lyrics continue).
//   - \gtab{...}{...}, \lilypondfile[...]{...} outside a verse/chorus,
//     blank lines, and whole-line "%" comments are ignored.
// Parse is ParseWithResolver with no .ly resolver — a section whose
// chords live only in a sibling LilyPond file (never as \[Chord] text)
// is left chordless, same as before lilychord existed.
func Parse(tex string) ([]ParsedSong, error) {
	return ParseWithResolver(tex, nil)
}

// ParseWithResolver is Parse, plus resolveLy: called with a referenced
// .ly filename (e.g. "bridge.ly") whenever a standalone \lilypondfile
// line is seen with no \[Chord] text already collected for that section
// — see lilypondStandaloneRE's own doc comment for why only a
// whole-line reference triggers this, and ParseFile for the real
// resolver (reads the sibling file and runs it through
// internal/lilychord.Parse). A nil resolveLy behaves exactly like Parse.
func ParseWithResolver(tex string, resolveLy func(filename string) []song.Measure) ([]ParsedSong, error) {
	var out []ParsedSong
	var cur *ParsedSong
	chordsOn := true
	pendingLabel := ""
	var sb *sectionBuilder

	flushSection := func() {
		if sb == nil || cur == nil {
			sb = nil
			return
		}
		sec := song.Section{Label: sb.label, Lyrics: sb.lines}
		if len(sb.chords) > 0 {
			sec.Measures = buildGrid(sb.chords)
		} else if len(sb.lyMeasures) > 0 {
			sec.Measures = sb.lyMeasures
		}
		cur.Song.Sections = append(cur.Song.Sections, sec)
		sb = nil
	}
	flushPendingLabel := func() {
		if pendingLabel != "" && cur != nil {
			cur.Song.Sections = append(cur.Song.Sections, song.Section{Label: pendingLabel})
		}
		pendingLabel = ""
	}

	for _, rawLine := range strings.Split(tex, "\n") {
		line := rawLine
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "%") {
			continue
		}

		if m := beginsongRE.FindStringSubmatch(line); m != nil {
			if cur != nil {
				flushSection()
				flushPendingLabel()
				out = append(out, *cur)
			}
			cur = &ParsedSong{
				Title:  unescapeLatex(strings.TrimSpace(m[1])),
				Artist: unescapeLatex(strings.TrimSpace(m[2])),
				Song:   &song.Song{},
			}
			// title/artist live in the body's own Meta (what the rest of
			// this app actually reads — see e.g. handlers_pages.go's
			// sg.Get("title") calls), not just these convenience fields.
			cur.Song.Set("title", cur.Title)
			if cur.Artist != "" {
				cur.Song.Set("artist", cur.Artist)
			}
			chordsOn = true
			pendingLabel = ""
			continue
		}
		if cur == nil {
			continue // outside any \beginsong block — a manifest/driver file line
		}
		if strings.Contains(line, `\endsong`) {
			flushSection()
			flushPendingLabel()
			if cur.Capo != "" {
				cur.Song.Set("capo", cur.Capo)
			}
			if cur.Transpose != "" {
				cur.Song.Set("transpose", cur.Transpose)
			}
			out = append(out, *cur)
			cur = nil
			continue
		}
		if m := capoRE.FindStringSubmatch(line); m != nil {
			cur.Capo = strings.TrimSpace(m[1])
			continue
		}
		if m := transposeRE.FindStringSubmatch(line); m != nil {
			cur.Transpose = strings.TrimSpace(m[1])
			continue
		}
		if strings.Contains(line, `\chordsoff`) {
			chordsOn = false
			continue
		}
		if strings.Contains(line, `\chordson`) {
			chordsOn = true
			continue
		}
		if m := textnoteRE.FindStringSubmatch(line); m != nil {
			flushSection()
			flushPendingLabel()
			pendingLabel = strings.TrimSpace(m[1])
			continue
		}
		if strings.Contains(line, `\beginverse`) {
			flushSection()
			label := pendingLabel
			if label == "" {
				label = "Verse"
			}
			pendingLabel = ""
			sb = &sectionBuilder{label: label}
			continue
		}
		if strings.Contains(line, `\beginchorus`) {
			flushSection()
			label := pendingLabel
			if label == "" {
				label = "Chorus"
			}
			pendingLabel = ""
			sb = &sectionBuilder{label: label}
			continue
		}
		if strings.Contains(line, `\endverse`) || strings.Contains(line, `\endchorus`) {
			flushSection()
			continue
		}
		if m := lilypondStandaloneRE.FindStringSubmatch(trimmed); m != nil {
			var measures []song.Measure
			if resolveLy != nil {
				measures = resolveLy(m[1])
			}
			if sb != nil {
				// Only ever used as a fallback for a section with no
				// \[Chord] text of its own (flushSection prefers
				// sb.chords when both exist) — most sections' own
				// \lilypondfile reference is just the engraving for
				// chords already written inline, redundant with chords
				// already collected.
				sb.lyMeasures = measures
				continue
			}
			// A standalone reference outside any \beginverse/\beginchorus
			// (e.g. Still Got The Blues' intro.ly, referenced before the
			// first \beginverse at all — or a \textnote{Bridge} INSIDE an
			// open \beginverse, which flushes that verse right away and
			// leaves sb nil, same as \textnote always does; see
			// Trio/still-got-the-blues' real bridge, whose label and
			// \lilypondfile both sit ahead of its lyric lines inside one
			// \beginverse block). Opens a real sectionBuilder, not a
			// finished Section appended outright — any lyric lines still
			// to come before the next \endverse/\beginverse must land in
			// THIS section, not be silently dropped for having no open
			// sb to attach to (a real bug this shape triggered: Still Got
			// The Blues' bridge lyrics vanished until this was an sb).
			flushSection()
			label := pendingLabel
			if label == "" {
				label = labelFromFilename(m[1])
			}
			pendingLabel = ""
			sb = &sectionBuilder{label: label, lyMeasures: measures}
			continue
		}
		if sb == nil {
			continue // between \beginsong and the first section — \gtab, preamble-ish lines
		}
		if m := nolyricsRE.FindStringSubmatch(trimmed); m != nil {
			for _, cm := range chordRE.FindAllStringSubmatch(m[1], -1) {
				sb.chords = append(sb.chords, normalizeChord(cm[1]))
			}
			continue
		}
		lyric, chords, hasText := processContentLine(line, chordsOn)
		sb.chords = append(sb.chords, chords...)
		if hasText {
			sb.lines = append(sb.lines, lyric)
		}
	}
	if cur != nil {
		flushSection()
		flushPendingLabel()
		if cur.Capo != "" {
			cur.Song.Set("capo", cur.Capo)
		}
		if cur.Transpose != "" {
			cur.Song.Set("transpose", cur.Transpose)
		}
		out = append(out, *cur)
	}
	return out, nil
}

// tempoRE matches a LilyPond \tempo statement with a quarter-note beats-
// per-minute value — the only tempo encoding this corpus's .ly files
// use ("\tempo 4 = 92"); see this package's own survey-derived doc
// comment on Parse for why nothing else (key, time signature) is
// reliably extractable from these files.
var tempoRE = regexp.MustCompile(`\\tempo\s+4\s*=\s*(\d+)`)

// TempoFromSiblingLy scans every *.ly file next to texPath for a
// \tempo 4 = N statement, returning the first one found — a song's
// several .ly files (intro/verse/chorus/bridge) are consistently the
// same BPM within one song in the real corpus, so "first match wins" is
// sufficient rather than needing to reconcile several. Returns "" if no
// .ly file or no \tempo statement is present (most of the corpus; tempo
// is otherwise unrecoverable from source, per the survey).
func TempoFromSiblingLy(texPath string) string {
	dir := filepath.Dir(texPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ly") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if m := tempoRE.FindSubmatch(b); m != nil {
			return string(m[1])
		}
	}
	return ""
}

// ParseFile reads and parses texPath, also filling in tempo: from a
// sibling .ly file when one states it (see TempoFromSiblingLy). Returns
// an error only for an unreadable file — a .tex with no \beginsong block
// at all (a manifest/driver/preamble file, not a song) returns an empty
// slice, not an error, since callers glob a whole tree and must be able
// to tell "not a song file" apart from "a song file we failed to read."
func ParseFile(texPath string) ([]ParsedSong, error) {
	b, err := os.ReadFile(texPath)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(texPath)
	resolveLy := func(filename string) []song.Measure {
		lyBody, err := os.ReadFile(filepath.Join(dir, filename))
		if err != nil {
			return nil
		}
		measures, err := lilychord.Parse(string(lyBody))
		if err != nil {
			return nil
		}
		return measures
	}
	songs, err := ParseWithResolver(string(b), resolveLy)
	if err != nil {
		return nil, err
	}
	tempo := TempoFromSiblingLy(texPath)
	if tempo != "" {
		for i := range songs {
			songs[i].Song.Set("tempo", tempo)
		}
	}
	return songs, nil
}
