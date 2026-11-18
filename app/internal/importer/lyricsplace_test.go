package importer

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"app/internal/song"
)

// All lyrics below are invented placeholder lines.

func TestSectionKind(t *testing.T) {
	for label, want := range map[string]string{
		"Verse 1": "verse", "Couplet 2": "verse", "Chorus": "chorus", "Refrain": "chorus",
		"Pre-Chorus": "pre-chorus", "Prechorus": "pre-chorus", "Pré-refrain": "pre-chorus", "Pre refrain 2": "pre-chorus",
		"Bridge": "bridge", "Pont": "bridge", "Middle 8": "bridge", "Intro": "intro", "Outro": "outro",
		"Coda": "outro", "Guitar Solo": "solo", "Solo (Chorus)": "solo", "Interlude": "interlude",
		"Instrumental": "interlude", "Post-Chorus": "post-chorus", "A": "", "Hook": "chorus",
	} {
		if got := SectionKind(label); got != want {
			t.Errorf("SectionKind(%q) = %q, want %q", label, got, want)
		}
	}
	if k := (Stanza{Label: "Tag"}).Kind(); k != "other" {
		t.Errorf("Kind of unknown label = %q", k)
	}
	if k := (Stanza{}).Kind(); k != "" {
		t.Errorf("Kind of unlabelled stanza = %q", k)
	}
}

func TestSplitStanzas(t *testing.T) {
	in := "Tabbed by: someone\n" +
		"[Verse 1: Some Singer]\n" +
		"[Am]walking down the [F]quiet road\n" +
		"Am   F   C   G\n" +
		"humming in the rain (x2)\n" +
		"\n\n" +
		"Refrain :\n" +
		"oh la la la\n" +
		"[G]oh la la la\n" +
		"x2\n" +
		"\n" +
		"Couplet 2\n" +
		"\n" +
		"counting all the paper stars\n" +
		"e|---3---|\n" +
		"under neon skies\n" +
		"\n" +
		"(Pré-refrain)\n" +
		"[C] [G]\n" +
		"almost there\n" +
		"\n" +
		"Chorus: oh la la la\n" +
		"oh la la la\n" +
		"\n" +
		"just a plain stanza\n" +
		"with no label\n" +
		"[Outro]\n" +
		"\n" +
		"https://example.com/lyrics\n"
	got := SplitStanzas(in)
	want := []Stanza{
		{Label: "Verse 1", Lines: []string{"walking down the quiet road", "humming in the rain"}},
		{Label: "Refrain", Lines: []string{"oh la la la", "oh la la la"}},
		{Label: "Couplet 2", Lines: []string{"counting all the paper stars", "under neon skies"}},
		{Label: "Pré-refrain", Lines: []string{"almost there"}},
		{Label: "Chorus", Lines: []string{"oh la la la", "oh la la la"}},
		{Label: "", Lines: []string{"just a plain stanza", "with no label"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitStanzas:\ngot  %#v\nwant %#v", got, want)
	}
}

// chart is an invented chart, grids only.
const chart = `title: Placeholder
artist: Nobody

[Intro]
grid: Am | F | C | G

[Verse 1]
grid: Am | F | C | G
note: softly

[Chorus]
grid: C | G | Am | F

[Verse 2]
grid: Am | F | C | G

[Chorus]
grid: C | G | Am | F

[Bridge]
grid: F | G | Em | Am

[Chorus]
grid: C | G | Am | F

[Outro]
grid: Am | F | C | G
`

var (
	v1 = []string{"walking down the quiet road", "humming in the rain"}
	ch = []string{"oh la la la", "the night is long"}
	v2 = []string{"counting all the paper stars", "under neon skies"}
	br = []string{"almost there", "almost home"}
)

// checkReparse parses before and after and checks the sections are the
// same (labels, repeat flags, grids, directives), and returns each
// section's lyrics as plain text lines.
func checkReparse(t *testing.T, before, after string) [][]string {
	t.Helper()
	a, b := song.Parse(before), song.Parse(after)
	if !reflect.DeepEqual(a.Meta, b.Meta) {
		t.Errorf("header changed: %v -> %v", a.Meta, b.Meta)
	}
	if len(a.Sections) != len(b.Sections) {
		t.Fatalf("section count %d -> %d", len(a.Sections), len(b.Sections))
	}
	var lyrics [][]string
	for i := range a.Sections {
		sa, sb := a.Sections[i], b.Sections[i]
		if sa.Label != sb.Label || sa.Repeat != sb.Repeat || sa.Times != sb.Times || sa.Time != sb.Time ||
			!reflect.DeepEqual(sa.Measures, sb.Measures) || !reflect.DeepEqual(sa.Notes, sb.Notes) ||
			!reflect.DeepEqual(sa.Scores, sb.Scores) {
			t.Errorf("section %d changed: %+v -> %+v", i, sa, sb)
		}
		var ls []string
		for _, l := range sb.Lyrics {
			var s strings.Builder
			for _, seg := range l.Segments {
				if seg.HasChord {
					t.Errorf("section %s: placed lyric carries a chord %q", sb.Label, seg.Chord)
				}
				s.WriteString(seg.Text)
			}
			ls = append(ls, s.String())
		}
		lyrics = append(lyrics, ls)
	}
	return lyrics
}

func placementsString(ps []Placement) string {
	var parts []string
	for _, p := range ps {
		parts = append(parts, fmt.Sprintf("%s=%s:%d", p.Section, p.Status, p.Stanza))
	}
	return strings.Join(parts, " ")
}

func TestPlaceLyricsLabelled(t *testing.T) {
	stanzas := []Stanza{
		{Label: "Verse 1", Lines: v1},
		{Label: "Chorus", Lines: ch},
		{Label: "Verse 2", Lines: v2},
		{Label: "Chorus", Lines: ch},
		{Label: "Bridge", Lines: br},
		{Label: "Chorus", Lines: ch},
	}
	body, ps, unused := PlaceLyrics(chart, stanzas, false)
	wantP := "Intro=instrumental:-1 Verse 1=placed:0 Chorus=placed:1 Verse 2=placed:2 Chorus=reused:1 Bridge=placed:4 Chorus=reused:1 Outro=instrumental:-1"
	if got := placementsString(ps); got != wantP {
		t.Errorf("placements:\ngot  %s\nwant %s", got, wantP)
	}
	if len(unused) != 0 {
		t.Errorf("unused = %v", unused)
	}
	lyrics := checkReparse(t, chart, body)
	want := [][]string{nil, v1, ch, v2, ch, br, ch, nil}
	for i := range want {
		if !reflect.DeepEqual(lyrics[i], want[i]) {
			t.Errorf("section %d lyrics = %q, want %q", i, lyrics[i], want[i])
		}
	}
	// Lyrics go right after the grid, before the note: line.
	if !strings.Contains(body, "[Verse 1]\ngrid: Am | F | C | G\n"+v1[0]+"\n"+v1[1]+"\nnote: softly\n") {
		t.Errorf("lyrics not right after the grid:\n%s", body)
	}
}

func TestPlaceLyricsUnlabelled(t *testing.T) {
	stanzas := []Stanza{{Lines: v1}, {Lines: ch}, {Lines: v2}, {Lines: ch}, {Lines: br}, {Lines: ch}}
	body, ps, unused := PlaceLyrics(chart, stanzas, false)
	wantP := "Intro=instrumental:-1 Verse 1=placed:0 Chorus=placed:1 Verse 2=placed:2 Chorus=reused:1 Bridge=placed:4 Chorus=reused:1 Outro=instrumental:-1"
	if got := placementsString(ps); got != wantP {
		t.Errorf("placements:\ngot  %s\nwant %s", got, wantP)
	}
	if len(unused) != 0 {
		t.Errorf("unused = %v", unused)
	}
	checkReparse(t, chart, body)
}

func TestPlaceLyricsFrench(t *testing.T) {
	src := "Couplet 1\nwalking down the quiet road\nhumming in the rain\n\n" +
		"Pré-refrain :\nalmost there\n\n" +
		"Refrain :\noh la la la\nthe night is long\n\n" +
		"Couplet 2\ncounting all the paper stars\nunder neon skies\n\n" +
		"Pont\nla la la\n"
	stanzas := SplitStanzas(src)
	if len(stanzas) != 5 {
		t.Fatalf("stanzas = %#v", stanzas)
	}
	body := "[Intro]\ngrid: C\n\n[Couplet 1]\ngrid: Am\n\n[Pré-refrain]\ngrid: F\n\n[Refrain]\ngrid: C\n\n" +
		"[Couplet 2]\ngrid: Am\n\n[Pré-refrain]\ngrid: F\n\n[Refrain]\ngrid: C\n\n[Pont]\ngrid: G\n\n[Refrain]\ngrid: C\n"
	got, ps, unused := PlaceLyrics(body, stanzas, false)
	wantP := "Intro=instrumental:-1 Couplet 1=placed:0 Pré-refrain=placed:1 Refrain=placed:2 Couplet 2=placed:3 " +
		"Pré-refrain=reused:1 Refrain=reused:2 Pont=placed:4 Refrain=reused:2"
	if p := placementsString(ps); p != wantP {
		t.Errorf("placements:\ngot  %s\nwant %s", p, wantP)
	}
	if len(unused) != 0 {
		t.Errorf("unused = %v", unused)
	}
	checkReparse(t, body, got)

	// The same French lyrics fit an English chart.
	_, ps, _ = PlaceLyrics(chart, stanzas, false)
	wantP = "Intro=instrumental:-1 Verse 1=placed:0 Chorus=placed:2 Verse 2=placed:3 Chorus=reused:2 Bridge=placed:4 Chorus=reused:2 Outro=instrumental:-1"
	if p := placementsString(ps); p != wantP {
		t.Errorf("English chart placements:\ngot  %s\nwant %s", p, wantP)
	}
}

func TestPlaceLyricsMoreStanzasThanSections(t *testing.T) {
	body := "[Verse]\ngrid: Am\n\n[Chorus]\ngrid: C\n"
	stanzas := []Stanza{
		{Label: "Verse 1", Lines: v1}, {Label: "Chorus", Lines: ch},
		{Label: "Verse 2", Lines: v2}, {Label: "Bridge", Lines: br}, {Lines: []string{"la la la"}},
	}
	got, ps, unused := PlaceLyrics(body, stanzas, false)
	if p := placementsString(ps); p != "Verse=placed:0 Chorus=placed:1" {
		t.Errorf("placements = %s", p)
	}
	if !reflect.DeepEqual(unused, []int{2, 3, 4}) {
		t.Errorf("unused = %v", unused)
	}
	checkReparse(t, body, got)
}

func TestPlaceLyricsFewerStanzas(t *testing.T) {
	stanzas := []Stanza{{Label: "Verse 1", Lines: v1}, {Label: "Chorus", Lines: ch}}
	got, ps, unused := PlaceLyrics(chart, stanzas, false)
	wantP := "Intro=instrumental:-1 Verse 1=placed:0 Chorus=placed:1 Verse 2=missing:-1 Chorus=reused:1 Bridge=missing:-1 Chorus=reused:1 Outro=instrumental:-1"
	if p := placementsString(ps); p != wantP {
		t.Errorf("placements:\ngot  %s\nwant %s", p, wantP)
	}
	if len(unused) != 0 {
		t.Errorf("unused = %v", unused)
	}
	checkReparse(t, chart, got)
}

func TestPlaceLyricsKeepsExisting(t *testing.T) {
	body := "[Verse 1]\ngrid: Am | F\nmy own first line\n# a comment\n\n[Chorus]\ngrid: C | G\n\n[Verse 2]\ngrid: Am | F\n\n[Chorus]\ngrid: C | G\nmy own chorus line\n"
	stanzas := []Stanza{{Label: "Verse 1", Lines: v1}, {Label: "Chorus", Lines: ch}, {Label: "Verse 2", Lines: v2}}

	got, ps, unused := PlaceLyrics(body, stanzas, false)
	if p := placementsString(ps); p != "Verse 1=kept:0 Chorus=placed:1 Verse 2=placed:2 Chorus=kept:1" {
		t.Errorf("placements = %s", p)
	}
	if len(unused) != 0 {
		t.Errorf("unused = %v", unused)
	}
	lyrics := checkReparse(t, body, got)
	want := [][]string{{"my own first line"}, ch, v2, {"my own chorus line"}}
	if !reflect.DeepEqual(lyrics, want) {
		t.Errorf("lyrics = %q, want %q", lyrics, want)
	}

	got, ps, _ = PlaceLyrics(body, stanzas, true)
	if p := placementsString(ps); p != "Verse 1=placed:0 Chorus=placed:1 Verse 2=placed:2 Chorus=reused:1" {
		t.Errorf("replace placements = %s", p)
	}
	lyrics = checkReparse(t, body, got)
	want = [][]string{v1, ch, v2, ch}
	if !reflect.DeepEqual(lyrics, want) {
		t.Errorf("replace lyrics = %q, want %q", lyrics, want)
	}
	if !strings.Contains(got, "# a comment") {
		t.Errorf("comment lost:\n%s", got)
	}
}

func TestPlaceLyricsRepeatAndBareReferences(t *testing.T) {
	body := "[Verse]\ngrid: Am | F\n\n[Chorus]\ngrid: C | G\n\n[Verse]\n\n[Chorus]\n\n[Chorus] (repeat)\n\n[Solo]\n"
	stanzas := []Stanza{{Label: "Verse 1", Lines: v1}, {Label: "Chorus", Lines: ch}, {Label: "Verse 2", Lines: v2}}
	got, ps, unused := PlaceLyrics(body, stanzas, false)
	want := "Verse=placed:0 Chorus=placed:1 Verse=placed:2 Chorus=reused:1 Chorus=reused:1 Solo=instrumental:-1"
	if p := placementsString(ps); p != want {
		t.Errorf("placements:\ngot  %s\nwant %s", p, want)
	}
	if len(unused) != 0 {
		t.Errorf("unused = %v", unused)
	}
	lyrics := checkReparse(t, body, got)
	if !reflect.DeepEqual(lyrics[2], v2) || lyrics[3] != nil || lyrics[4] != nil {
		t.Errorf("lyrics = %q", lyrics)
	}
}

func TestPlaceLyricsSanitizesLines(t *testing.T) {
	body := "[Verse]\ngrid: Am\n"
	stanzas := []Stanza{{Label: "Verse", Lines: []string{"# hash at the start", "| pipe at the start", "time: to go", "[spoken] la la la", "[Chorus]"}}}
	got, _, _ := PlaceLyrics(body, stanzas, false)
	lyrics := checkReparse(t, body, got)
	want := []string{"hash at the start", "pipe at the start", "Time: to go", "(spoken) la la la", "(Chorus)"}
	if !reflect.DeepEqual(lyrics[0], want) {
		t.Errorf("lyrics = %q, want %q\n%s", lyrics[0], want, got)
	}
}

func TestPlaceLyricsAfterCleanup(t *testing.T) {
	// The intended flow: clean an import, then place fetched lyrics.
	draft := "[Intro]\ngrid: Am | F\nTabbed by someone\n\n[Verse 1]\ngrid: Am | F | C | G\n[Am]old words [F]here\n\n[Chorus]\ngrid: C | G\nChorus:\n[C]old chorus\n"
	clean, _ := CleanSongText(draft, CleanOptions{StripInlineChords: true})
	lyricsText := "[Verse 1]\nwalking down the quiet road\n\n[Chorus]\noh la la la\n"
	got, ps, _ := PlaceLyrics(clean, SplitStanzas(lyricsText), true)
	if p := placementsString(ps); p != "Intro=instrumental:-1 Verse 1=placed:0 Chorus=placed:1" {
		t.Errorf("placements = %s", p)
	}
	want := "[Intro]\ngrid: Am | F\n\n[Verse 1]\ngrid: Am | F | C | G\nwalking down the quiet road\n\n[Chorus]\ngrid: C | G\noh la la la\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPlaceLyricsNoStanzasIsNoOp(t *testing.T) {
	got, ps, unused := PlaceLyrics(chart, nil, true)
	if got != chart {
		t.Errorf("body changed:\n%s", got)
	}
	if len(ps) != 8 || len(unused) != 0 {
		t.Errorf("placements=%v unused=%v", ps, unused)
	}
}
