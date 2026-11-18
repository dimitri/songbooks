package songimport

import (
	"context"
	"fmt"
	"strings"

	"app/internal/importer"
	"app/internal/store"
)

// Lyrics sources, one song at a time: the best Ultimate Guitar chart's
// lyrics (with its section labels), a lyrics page the person gives, or
// text they paste.
const (
	LyricsFromUG   = "ug"
	LyricsFromURL  = "url"
	LyricsFromText = "text"
	// LyricsFromOwn moves the lyrics already in the song into the right
	// sections where an import ran two parts together (importer.ResplitOwn).
	LyricsFromOwn = "own"
)

// Stanzas fetches a song's lyrics from source and splits them into
// stanzas. from says where they came from, for the person to check.
func Stanzas(ctx context.Context, source, title, artist, input string) (stanzas []importer.Stanza, from string, err error) {
	switch source {
	case LyricsFromUG, "":
		res, err := importer.FindBestUGTab(title, artist)
		if err != nil {
			return nil, "", err
		}
		for _, sec := range res.Song.Sections {
			text := strings.TrimSpace(sec.Lyrics())
			if text == "" {
				continue
			}
			// The section's own label, then its lines as SplitStanzas
			// cleans them (chord-only lines, noise).
			for _, s := range importer.SplitStanzas(text) {
				if s.Label == "" {
					s.Label = sec.Label
				}
				stanzas = append(stanzas, s)
			}
		}
		from = res.URL
		if !res.Verified {
			from += " (not cross-checked)"
		}
	case LyricsFromURL:
		text, src, err := importer.FetchLyrics(input)
		if err != nil {
			return nil, "", err
		}
		stanzas, from = importer.SplitStanzas(text), src
	case LyricsFromText:
		stanzas, from = importer.SplitStanzas(input), "pasted text"
	default:
		return nil, "", fmt.Errorf("unknown lyrics source %q", source)
	}
	if len(stanzas) == 0 {
		return nil, from, fmt.Errorf("no lyrics found")
	}
	return stanzas, from, nil
}

// LyricsResult is lyrics placed in a chart: the new text, where each
// stanza went, and what was left over.
type LyricsResult struct {
	Text       string               `json:"text"`
	Placements []importer.Placement `json:"placements"`
	Unused     []int                `json:"unused"`
	Stanzas    int                  `json:"stanzas"`
	From       string               `json:"from"`
	Notes      []string             `json:"notes,omitempty"`
}

// PlaceLyrics fetches lyrics and writes them into body's sections (see
// importer.PlaceLyrics), keeping the lyrics already there unless replace.
func PlaceLyrics(ctx context.Context, body, source, title, artist, input string, replace bool) (*LyricsResult, error) {
	if source == LyricsFromOwn {
		return resplitOwn(body)
	}
	stanzas, from, err := Stanzas(ctx, source, title, artist, input)
	if err != nil {
		return nil, err
	}
	text, placements, unused := importer.PlaceLyrics(body, stanzas, replace)
	return &LyricsResult{Text: text, Placements: placements, Unused: unused, Stanzas: len(stanzas), From: from}, nil
}

// Draft is a chart fetched for review: its text, where it came from and
// what was done to it.
type Draft struct {
	Text   string   `json:"text"`
	URL    string   `json:"url"`
	Title  string   `json:"title"`
	Artist string   `json:"artist"`
	Capo   string   `json:"capo,omitempty"`
	IReal  string   `json:"ireal,omitempty"` // the iReal Pro chart used, "" when none
	Notes  []string `json:"notes"`
}

// VocalDraft is the singer's chart: the lyrics with their sections from
// the best Ultimate Guitar version, without chords in them, and the
// grids from iReal Pro where its chart matches (Ultimate Guitar's
// grids are kept where it doesn't, and said so), cleaned up.
func VocalDraft(ctx context.Context, st *store.Store, title, artist string) (*Draft, error) {
	res, err := importer.FindBestUGTab(title, artist)
	if err != nil {
		return nil, err
	}
	all := make([]int, len(res.Song.Sections))
	for i := range all {
		all[i] = i
	}
	d := &Draft{URL: res.URL, Title: res.Song.Title, Artist: res.Song.Artist, Capo: res.Song.Capo}
	if !res.Verified {
		d.Notes = append(d.Notes, "the lyrics weren't cross-checked: read them through")
	}
	text, notes := importer.CleanSongText(res.Song.ToSongText(all), importer.CleanOptions{StripInlineChords: true})
	d.Notes = append(d.Notes, notes...)
	d.Text = withIReal(ctx, st, d, text, title, artist)
	return d, nil
}

// withIReal applies iReal Pro's grids to a draft's sections where its
// chart is a safe match (Ultimate Guitar's stay elsewhere, said so) and
// takes its time signature.
func withIReal(ctx context.Context, st *store.Store, d *Draft, text, title, artist string) string {
	merged, cands, info, err := IRealFix(ctx, st, text, title, artist, false)
	if err != nil {
		d.Notes = append(d.Notes, "no iReal Pro chart found: the grids are Ultimate Guitar's")
		return text
	}
	d.IReal = info.SourceURL
	used, other := 0, 0
	for _, c := range cands {
		if c.Safe {
			used++
		} else {
			other++
		}
	}
	d.Notes = append(d.Notes, fmt.Sprintf("iReal Pro grids used in %d sections", used))
	if other > 0 {
		d.Notes = append(d.Notes, fmt.Sprintf("%d sections keep Ultimate Guitar's grid (iReal's differs or is in another key): check them in the iReal step", other))
	}
	if info.TimeSig != "" {
		merged = SetHeader(merged, "time", info.TimeSig, false)
	}
	return merged
}

// resplitOwn moves the song's own lyrics into the right sections where
// an import ran two parts together (importer.ResplitOwn): nothing is
// dropped or rewritten, and a part with nowhere to go stays put.
func resplitOwn(body string) (*LyricsResult, error) {
	text, placements, notes := importer.ResplitOwn(body)
	return &LyricsResult{Text: text, Placements: placements, Stanzas: 0, From: "this song's own lyrics", Notes: notes}, nil
}
