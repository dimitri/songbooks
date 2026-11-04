package importer

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
)

// UGToken is one inline chord+lyric fragment from a tab line: the chord
// that starts it (empty for a line/section with no chord at that point)
// plus the lyric text up to the next chord or end of line.
type UGToken struct {
	Chord string
	Text  string
}

// UGLine is one lyric line, already split into its inline chord/lyric
// tokens — this is the semantic form both the grid-builder and the
// lyric-line serializer below read from, instead of re-scanning raw text.
type UGLine struct {
	Tokens []UGToken
}

// UGSection is one [Verse]/[Chorus]-labelled block of a tab. Label is ""
// for text appearing before the tab's first section marker (common on
// UG — an intro riff or a stray chord line with no heading of its own).
type UGSection struct {
	Label string
	Lines []UGLine
}

// UGSong is the semantic form a raw Ultimate Guitar tab parses into —
// deliberately lossy (most tabs don't encode bar placement at all, see
// Chords on UGLine: this app's own grid: syntax wants bars, UG's plain
// text only ever gives a left-to-right chord sequence), but structured
// enough to drive a real preview and a naive grid conversion, instead of
// landing one undifferentiated blob of text in a raw textarea.
type UGSong struct {
	Title    string
	Artist   string
	Sections []UGSection
}

// sectionHeaderRE matches a line that is ENTIRELY one bracketed label
// ("[Verse 1]", "[Chorus]") and nothing else — the shape UG tab text
// uses for section headings, after chord tags have already been
// converted to plain "[Chord]" below (see ugLineTokenRE's comment for
// why that conversion has to happen before this check, not after).
var sectionHeaderRE = regexp.MustCompile(`^\[([^\]]+)\]$`)

// ugLineTokenRE scans a line for "[Chord]lyric" runs: a bracketed group
// followed by the plain text up to the next bracket (or end of line).
// Applied AFTER chTagRE has turned UG's own "[ch]G[/ch]" wrapper into our
// "[G]" shape, so this one regex handles both a tagged chord and the
// section-header case (an empty Text after it) — the header check above
// runs first per line, before this ever sees it.
var ugLineTokenRE = regexp.MustCompile(`\[([^\]]*)\]([^\[]*)`)

// chordLikeRE decides whether a bracketed token found mid-line is a
// chord symbol (A-G root, then only chord-quality-shaped characters) as
// opposed to incidental bracketed text — UG tab text has no other use of
// brackets once chord tags are converted, so in practice this only ever
// has to reject nothing, but the check stays defensive rather than
// assuming it.
var chordQualityRE = regexp.MustCompile(`^(?:maj|min|mi|dim|aug|sus|add|[mM#b0-9+°ø△/A-Gb])*$`)

func looksLikeChord(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "%" || s == "N.C." {
		return true
	}
	if len(s) == 0 {
		return false
	}
	c := s[0]
	if c < 'A' || c > 'G' {
		return false
	}
	rest := s[1:]
	if rest != "" && (rest[0] == 'b' || rest[0] == '#') {
		rest = rest[1:]
	}
	return chordQualityRE.MatchString(rest)
}

// chordTagGroupRE matches one whole "[ch]X[/ch]" group in RAW (unconverted)
// UG tab text, capturing the chord text itself (group 1).
var chordTagGroupRE = regexp.MustCompile(`\[ch\]([^\[]*)\[/ch\]`)

// chordOnlyRawLineRE matches a raw line (tags intact) that is nothing but
// one or more "[ch]X[/ch]" groups and whitespace — UG's own "chord row"
// convention: many tabs put a line of chords on its own line, spaced to
// align over the lyric line immediately below, rather than tagging chords
// inline within the lyric line itself (the ONLY shape ParseUGContent used
// to understand; see mergeChordRows's doc comment for why that silently
// produced two-line "chord-line-above-lyric-line" output instead of this
// app's own inline "[Chord]word" syntax).
var chordOnlyRawLineRE = regexp.MustCompile(`^(?:\s*\[ch\][^\[]*\[/ch\])+\s*$`)

// bareColumnsOf returns, in order, each "[ch]X[/ch]" group's chord text
// and the character column it starts at once every [ch]/[/ch] tag on the
// line is stripped down to just the bare chord text (no tags, no
// brackets) — the column space a tab's author actually typed in, since
// UG's own site strips tags for display and a chord row's spacing is
// authored to align with the plain lyric text below it, not with the
// (much longer) raw [ch]/[/ch]-tagged source text.
func bareColumnsOf(line string) (chords []string, cols []int) {
	stripped := 0
	for _, m := range chordTagGroupRE.FindAllStringSubmatchIndex(line, -1) {
		start, cStart, cEnd, end := m[0], m[2], m[3], m[1]
		chords = append(chords, line[cStart:cEnd])
		cols = append(cols, start-stripped)
		stripped += (end - start) - (cEnd - cStart)
	}
	return
}

// mergeChordRows folds UG's "chord row on its own line, lyric line right
// below it" convention into one merged raw line per pair, chord tags
// re-inserted at the column each one was authored at (see bareColumnsOf) —
// turning e.g.
//
//	[ch]G[/ch]          [ch]F[/ch]
//	They would hurt, They would hurt
//
// into
//
//	They would hurt,[ch]F[/ch] They would hurt
//
// with the FIRST chord's column folded in specially: a chord-row's very
// first chord almost always marks the line's first word (column 0),
// which this always honors by prepending it, regardless of its
// authored column — tab authors are inconsistent about whether that
// first chord is typed at column 0 exactly or a column or two into
// leading whitespace. Every subsequent chord on the row uses its real
// column, clamped to the lyric line's length, inserted into the nearest
// following word boundary rather than mid-word (splitting a word in two
// is never correct — any imprecision should land before a word, not
// inside one). A chord whose column is level with or past the end of
// the lyric line is appended at the end instead of dropped — rare, but
// losing a transcribed chord silently would be worse than misplacing it
// by a word or two, and this is explicitly a correctable draft (see this
// package's own doc comment), not an authoritative import.
// wordStartsOf returns the index of every word's first character in s
// (0, and every index right after a run of spaces) — the only positions
// a chord tag may ever be inserted at, since this app's inline syntax
// attaches a chord to the WORD it precedes, never to a position inside
// one.
func wordStartsOf(s string) []int {
	var starts []int
	atStart := true
	for i, c := range s {
		if c == ' ' {
			atStart = true
			continue
		}
		if atStart {
			starts = append(starts, i)
			atStart = false
		}
	}
	return starts
}

// nearestWordStart picks whichever entry in starts (word-start positions
// in a line of length n) is closest to col — a tab author's chord column
// marks the syllable it changes on, which often lands mid-word rather
// than exactly at a word's first letter (this app's model can only
// attach a chord to a whole word, never mid-word), so the closest word
// start, forward OR backward, is the best single-word approximation of
// where that chord belongs. n itself (append at the very end) is also a
// candidate, for a chord column past the last word. Ties favor the
// earlier (backward) word, matching the common case of a chord column
// sitting a character or two past the word it actually marks.
func nearestWordStart(col int, starts []int, n int) int {
	best, bestDist := n, abs(col-n)
	for _, s := range starts {
		if d := abs(col - s); d < bestDist {
			best, bestDist = s, d
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func mergeChordRows(lines []string) []string {
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !chordOnlyRawLineRE.MatchString(line) || i+1 >= len(lines) {
			out = append(out, line)
			continue
		}
		next := lines[i+1]
		if next == "" || chordOnlyRawLineRE.MatchString(next) || sectionHeaderRE.MatchString(strings.TrimRight(next, " \t")) {
			out = append(out, line)
			continue
		}
		chords, cols := bareColumnsOf(line)
		if len(chords) == 0 {
			out = append(out, line)
			continue
		}
		merged := next
		wordStarts := wordStartsOf(merged)
		for ci := len(chords) - 1; ci >= 0; ci-- {
			tag := "[ch]" + chords[ci] + "[/ch]"
			col := nearestWordStart(cols[ci], wordStarts, len(merged))
			merged = merged[:col] + tag + merged[col:]
		}
		out = append(out, merged)
		i++ // consumed both lines
	}
	return out
}

// ParseUGContent parses raw Ultimate Guitar tab content (UG's own
// "[ch]G[/ch]lyric" / "[Verse]" text, as stored in their page data,
// still carrying [ch]/[tab] tags) into the semantic UGSong form. Handles
// both UG layout conventions: chords tagged inline within the lyric line
// itself, and a chord-only row immediately above its lyric line (folded
// into inline form first — see mergeChordRows).
func ParseUGContent(raw string) (*UGSong, error) {
	raw = regexp.MustCompile(`\[/?tab\]`).ReplaceAllString(raw, "")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	mergedLines := mergeChordRows(strings.Split(raw, "\n"))

	content := chTagRE.ReplaceAllStringFunc(strings.Join(mergedLines, "\n"), func(m string) string {
		if m == "[ch]" {
			return "["
		}
		return "]"
	})

	song := &UGSong{}
	cur := UGSection{}
	haveSection := false
	flush := func() {
		if len(cur.Lines) > 0 || cur.Label != "" {
			song.Sections = append(song.Sections, cur)
		}
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}
		if m := sectionHeaderRE.FindStringSubmatch(line); m != nil && !looksLikeChord(m[1]) {
			flush()
			cur = UGSection{Label: strings.TrimSpace(m[1])}
			haveSection = true
			continue
		}
		var ln UGLine
		matches := ugLineTokenRE.FindAllStringSubmatchIndex(line, -1)
		// Any plain text before the line's first bracket (never possible
		// in the single-line "[ch]Chord[/ch]lyric" shape, where a chord
		// always opens its own segment — but routine now that
		// mergeChordRows can insert a chord mid-line, after real leading
		// words the regex below would otherwise silently drop) becomes
		// its own chordless leading token.
		if len(matches) == 0 || matches[0][0] > 0 {
			end := len(line)
			if len(matches) > 0 {
				end = matches[0][0]
			}
			ln.Tokens = append(ln.Tokens, UGToken{Text: line[:end]})
		}
		for _, mi := range matches {
			chord, text := line[mi[2]:mi[3]], line[mi[4]:mi[5]]
			if chord != "" && !looksLikeChord(chord) {
				// A bracketed run that isn't actually a chord (rare,
				// e.g. a stray annotation) — keep it as plain text
				// rather than silently dropping it.
				text = "[" + chord + "]" + text
				chord = ""
			}
			ln.Tokens = append(ln.Tokens, UGToken{Chord: chord, Text: text})
		}
		if len(ln.Tokens) == 0 {
			ln.Tokens = []UGToken{{Text: line}}
		}
		cur.Lines = append(cur.Lines, ln)
	}
	flush()
	if !haveSection && len(song.Sections) == 1 {
		song.Sections[0].Label = "Verse"
	}
	if len(song.Sections) == 0 {
		return nil, fmt.Errorf("no lyric/chord lines found in this tab")
	}
	return song, nil
}

// Chords returns every distinct chord token on this section's lines, in
// order of appearance (repeats included) — the naive "one bar per
// chord" sequence fitForGrid converts to a grid: line. Consecutive
// repeats of the same chord collapse to one bar with a "%" continuation,
// which is both the standard notation for "same chord held" AND a
// reasonable default when the tab doesn't actually mark a chord change
// (most don't say how long a chord lasts at all).
func (s UGSection) Chords() []string {
	var out []string
	for _, ln := range s.Lines {
		for _, tok := range ln.Tokens {
			if tok.Chord != "" {
				out = append(out, tok.Chord)
			}
		}
	}
	return out
}

// Grid renders this section's chord sequence as a naive grid: line — one
// bar per chord change, "%" for an immediate repeat. This is explicitly
// the "something naive... fixing that in the editor would be easy
// enough" starting point: real bar placement (how many beats each chord
// actually holds) isn't recoverable from plain UG tab text at all.
func (s UGSection) Grid() string {
	chords := s.Chords()
	if len(chords) == 0 {
		return ""
	}
	bars := make([]string, 0, len(chords))
	for i, c := range chords {
		if i > 0 && c == chords[i-1] {
			bars = append(bars, "%")
			continue
		}
		bars = append(bars, c)
	}
	return strings.Join(bars, " | ")
}

// Lyrics renders this section's lines back out in this app's own inline
// "[Chord]word" syntax (see song.typ's grammar: "Lyric lines: inline
// [Chord]word") — already the same bracket shape UG uses once its own
// [ch] tags are converted, so this is close to a no-op reassembly, not a
// reformat.
func (s UGSection) Lyrics() string {
	var b strings.Builder
	first := true
	for _, ln := range s.Lines {
		hasText := false
		for _, tok := range ln.Tokens {
			if strings.TrimSpace(tok.Text) != "" {
				hasText = true
				break
			}
		}
		// A line with no real lyric text at all — just a chord or a
		// whole chord sequence on its own (e.g. an instrumental intro's
		// "[Am] [G] [F] [Am]") — adds nothing Grid() doesn't already
		// capture, and reproducing it here as a bare "[Am] [G] [F] [Am]"
		// lyric line is actively harmful: this app's own .song parser
		// reads a section's lyric text expecting prose, not a run of
		// several bracket groups on one line, which it can misparse as a
		// garbled section header (several "[...]" runs read as one,
		// since a label is normally just one).
		if !hasText {
			continue
		}
		if !first {
			b.WriteByte('\n')
		}
		first = false
		for _, tok := range ln.Tokens {
			if tok.Chord != "" {
				b.WriteByte('[')
				b.WriteString(tok.Chord)
				b.WriteByte(']')
			}
			b.WriteString(tok.Text)
		}
	}
	return b.String()
}

// ToSongText renders the chosen sections (by index into song.Sections)
// as this app's own .song-file section/grid/lyrics text — the
// "converter to our own format" the import wizard's preview step
// confirms before this ever touches the editor.
func (song *UGSong) ToSongText(selected []int) string {
	var b strings.Builder
	for _, i := range selected {
		if i < 0 || i >= len(song.Sections) {
			continue
		}
		sec := song.Sections[i]
		label := sec.Label
		if label == "" {
			label = "Verse"
		}
		b.WriteString("[" + label + "]\n")
		if grid := sec.Grid(); grid != "" {
			b.WriteString("grid: " + grid + "\n")
		}
		if lyrics := sec.Lyrics(); strings.TrimSpace(lyrics) != "" {
			b.WriteString(lyrics + "\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()) + "\n"
}

// fetchUGPage fetches an Ultimate Guitar tab page and returns its raw
// tab content plus whatever title/artist UG's own page data carries
// (best-effort — an empty string if the page doesn't have them, never a
// hard failure over missing metadata).
func fetchUGPage(pageURL string) (content, title, artist string, err error) {
	body, err := fetch(pageURL)
	if err != nil {
		return "", "", "", err
	}
	m := ugStoreRE.FindStringSubmatch(body)
	if m == nil {
		return "", "", "", fmt.Errorf("couldn't find tab data on this page — Ultimate Guitar may have changed its layout, or this isn't a tab page")
	}
	raw := html.UnescapeString(m[1])

	var store struct {
		Store struct {
			Page struct {
				Data struct {
					Tab struct {
						SongName   string `json:"song_name"`
						ArtistName string `json:"artist_name"`
					} `json:"tab"`
					TabView struct {
						WikiTab struct {
							Content string `json:"content"`
						} `json:"wiki_tab"`
					} `json:"tab_view"`
				} `json:"data"`
			} `json:"page"`
		} `json:"store"`
	}
	if err := json.Unmarshal([]byte(raw), &store); err != nil {
		return "", "", "", fmt.Errorf("couldn't parse tab data: %w", err)
	}
	content = store.Store.Page.Data.TabView.WikiTab.Content
	if content == "" {
		return "", "", "", fmt.Errorf("this page has no tab content (chord diagrams / Pro tabs aren't supported — use a plain chord/lyric tab page)")
	}
	return content, store.Store.Page.Data.Tab.SongName, store.Store.Page.Data.Tab.ArtistName, nil
}

// FetchAndParseUGTab fetches pageURL and parses it straight into the
// semantic UGSong form — the wizard's "fetch and parse" step. Unlike
// FetchUGTab (kept for the existing raw-text importer button), this
// never falls back to raw text: a tab that fails to parse into at least
// one section is reported as an error.
func FetchAndParseUGTab(pageURL string) (*UGSong, error) {
	content, title, artist, err := fetchUGPage(pageURL)
	if err != nil {
		return nil, err
	}
	song, err := ParseUGContent(content)
	if err != nil {
		return nil, err
	}
	song.Title, song.Artist = title, artist
	return song, nil
}
