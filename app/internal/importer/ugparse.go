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

// ParseUGContent parses raw Ultimate Guitar tab content (UG's own
// "[ch]G[/ch]lyric" / "[Verse]" text, as stored in their page data,
// still carrying [ch]/[tab] tags) into the semantic UGSong form.
func ParseUGContent(raw string) (*UGSong, error) {
	content := chTagRE.ReplaceAllStringFunc(raw, func(m string) string {
		if m == "[ch]" {
			return "["
		}
		return "]"
	})
	content = regexp.MustCompile(`\[/?tab\]`).ReplaceAllString(content, "")
	content = strings.ReplaceAll(content, "\r\n", "\n")

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
		for _, tm := range ugLineTokenRE.FindAllStringSubmatch(line, -1) {
			chord, text := tm[1], tm[2]
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
	for i, ln := range s.Lines {
		if i > 0 {
			b.WriteByte('\n')
		}
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
