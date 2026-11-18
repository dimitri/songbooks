package importer

import (
	"strings"
	"testing"

	"app/internal/song"
)

// All lyrics below are invented placeholder lines.

type cleanCase struct {
	name  string
	opts  CleanOptions
	in    string
	want  string
	note  string // a substring one of the notes must contain ("" = no check)
	quiet bool   // expect no notes at all
}

var cleanCases = []cleanCase{
	{
		name: "capo moves to header",
		in:   "title: Quiet Road\n\n[Verse]\ngrid: C | G\nCapo 2nd fret\nwalking down the quiet road\n",
		want: "title: Quiet Road\ncapo: 2\n\n[Verse]\ngrid: C | G\nwalking down the quiet road\n",
		note: "capo: header",
	},
	{
		name: "capo line before first section, no header",
		in:   "Capo 3\n[Verse]\ngrid: C\nla la la\n",
		want: "capo: 3\n\n[Verse]\ngrid: C\nla la la\n",
		note: "capo",
	},
	{
		name: "capo already set is kept",
		in:   "title: X\ncapo: 1\n\n[Verse]\ngrid: C\nCapo: 4\nla la la\n",
		want: "title: X\ncapo: 1\n\n[Verse]\ngrid: C\nla la la\n",
		note: "already sets capo",
	},
	{
		name: "noise lines",
		in: "[Verse]\ngrid: C | G\nTabbed by: someone\nChords used: C G Am F\nTuning: standard\nStandard tuning\n****\n" +
			"see https://example.com/tab\nPlease rate this tab\nla la la\nEm7  0-2-2-0-3-3\n",
		want: "[Verse]\ngrid: C | G\nla la la\n",
		note: "noise line",
	},
	{
		name: "tab staff lines",
		in:   "[Intro]\ngrid: Am | G\ne|---3---|-----|\nB|---0---|--1--|\nG|-0h2---|\n|---5---7---|\nla la la\n",
		want: "[Intro]\ngrid: Am | G\nla la la\n",
		note: "tab staff",
	},
	{
		name: "chord-only line dropped when grid exists",
		in:   "[Verse]\ngrid: Am | F | C | G\nAm   F   C   G\nwalking down the quiet road\n[Am] [F]\n",
		want: "[Verse]\ngrid: Am | F | C | G\nwalking down the quiet road\n",
		note: "grid already has",
	},
	{
		name: "chord-only line becomes the grid",
		in:   "[Intro]\nIntro: Am G F x2\n\n[Verse]\nla la la\n",
		want: "[Intro]\ngrid: |: Am | G | F :|\n\n[Verse]\nla la la\n",
		note: "into the grid",
	},
	{
		name: "chord dictionary after last repeat close",
		in:   "[Verse]\ngrid: |: C | G :| Am | F\nla la la\n\n[Outro]\ngrid: |: Am | F :| Bb6 | Cmaj9 | D7sus4 | Ebdim | F#m7b5 | G9 | Abmaj7 | Bbm6 | C#7 | Dadd9 | E7#9 | Gm11 | Asus2 | Db6\n",
		want: "[Verse]\ngrid: |: C | G :| Am | F\nla la la\n\n[Outro]\ngrid: |: Am | F :|\n",
		note: "14-chord list",
	},
	{
		name: "chord dictionary at the end of the song, spread over grid lines",
		in:   "[Chorus]\ngrid: C | G | Am | F\nla la la\n\n[Outro]\ngrid: C | G | C\n| Bb6 | Cmaj9 | D7sus4 | Ebdim | F#m7b5 | G9\n| Abmaj7 | Bbm6 | C#7 | Dadd9 | E7#9 | Gm11\n",
		want: "[Chorus]\ngrid: C | G | Am | F\nla la la\n\n[Outro]\ngrid: C | G | C\n",
		note: "12-chord list",
	},
	{
		name:  "short or familiar chord tail is not a dictionary",
		in:    "[Verse]\ngrid: C | G | Am | F\nla la la\n\n[Outro]\ngrid: |: C | G :| Am | F | C | G | Am | F | C | G | Am | F | C | G\n",
		want:  "[Verse]\ngrid: C | G | Am | F\nla la la\n\n[Outro]\ngrid: |: C | G :| Am | F | C | G | Am | F | C | G | Am | F | C | G\n",
		quiet: true,
	},
	{
		name: "repeat marks on lyric lines",
		in:   "[Chorus]\ngrid: C | G\nla la la (x2)\nwalking down the quiet road x4\nhumming in the rain [x3]\n",
		want: "[Chorus]\ngrid: C | G\nla la la\nwalking down the quiet road\nhumming in the rain\n",
		note: "repeat mark",
	},
	{
		name: "standalone repeat ending a section becomes times",
		in:   "[Chorus]\ngrid: C | G | Am | F\nla la la\nwalking down the quiet road\n(x2)\n",
		want: "[Chorus]\ngrid: C | G | Am | F\nla la la\nwalking down the quiet road\ntimes: 2\n",
		note: "times: 2",
	},
	{
		name: "bracketed [x4] repeat is not a section",
		in:   "[Chorus]\ngrid: C | G\nla la la\n[x4]\n\n[Verse]\ngrid: Am\nwalking down the quiet road\n",
		want: "[Chorus]\ngrid: C | G\nla la la\ntimes: 4\n\n[Verse]\ngrid: Am\nwalking down the quiet road\n",
		note: "times: 4",
	},
	{
		name: "standalone repeat with a bracketed grid is dropped",
		in:   "[Chorus]\ngrid: |: C | G :|\nla la la\nx2\n",
		want: "[Chorus]\ngrid: |: C | G :|\nla la la\n",
		note: "standalone repeat",
	},
	{
		name: "label lines",
		in:   "[Chorus]\ngrid: C | G\nChorus:\nla la la\n\n[Verse 2]\ngrid: Am\nVERSE 2\n(Intro)\nwalking down the quiet road\n",
		want: "[Chorus]\ngrid: C | G\nla la la\n\n[Verse 2]\ngrid: Am\nwalking down the quiet road\n",
		note: "only named a section",
	},
	{
		name: "label with lyrics after colon is kept",
		in:   "[Verse]\ngrid: C\nChorus: la la la\n",
		want: "[Verse]\ngrid: C\nChorus: la la la\n",
	},
	{
		name: "blank runs and trailing spaces",
		in:   "title: X   \n\n\n[Verse]   \n\n\ngrid: C | G   \n\n\n\nla la la   \n\n\nwalking down the quiet road\n\n\n\n[Chorus]\ngrid: F\nhumming\n\n\n",
		want: "title: X   \n\n[Verse]\ngrid: C | G\n\nla la la\n\nwalking down the quiet road\n\n[Chorus]\ngrid: F\nhumming\n",
	},
	{
		name: "emptied sections dropped, bare markers, references, solo and repeat kept",
		in:   "[Intro]\ne|---3---|\n\n[Chorus]\ngrid: C | G\nla la la\n\n[Interlude]\n\n[Chorus]\nChorus\n\n[Solo]\nwww.example.com\n\n[Verse] (repeat)\nTabbed by someone\n\n[Bridge]\n\n[Outro]\nTabbed by someone\n",
		want: "[Chorus]\ngrid: C | G\nla la la\n\n[Interlude]\n\n[Chorus]\n\n[Solo]\n\n[Verse] (repeat)\n\n[Bridge]\n",
		note: "dropped empty section Outro",
	},
	{
		name: "strip inline chords",
		opts: CleanOptions{StripInlineChords: true},
		in:   "[Verse]\ngrid: Am | F\n[Am]walking down the [F]quiet road\nla [G] la la\n",
		want: "[Verse]\ngrid: Am | F\nwalking down the quiet road\nla la la\n",
		note: "inline chords",
	},
	{
		name: "inline chords kept by default",
		in:   "[Verse]\ngrid: Am | F\n[Am]walking down the [F]quiet road\n",
		want: "[Verse]\ngrid: Am | F\n[Am]walking down the [F]quiet road\n",
	},
	{
		name: "header lines, comments, directives and score blocks untouched",
		in:   "title: X\nTabbed by: someone\nchord: Am x02210\n\n# imported from somewhere\n[Verse]\ntime: 3/4\nbars: 3\nnote: softly\nscore[gtr]: \\new TabStaff {\n  e4 f g\n\n  Tabbed by nobody\n}\ngrid: C | G\nla la la\n# keep me\n",
		want: "title: X\nTabbed by: someone\nchord: Am x02210\n\n# imported from somewhere\n\n[Verse]\ntime: 3/4\nbars: 3\nnote: softly\nscore[gtr]: \\new TabStaff {\n  e4 f g\n\n  Tabbed by nobody\n}\ngrid: C | G\nla la la\n# keep me\n",
	},
	{
		name:  "sections never reordered, clean input unchanged",
		in:    "title: X\n\n[Verse 2]\ngrid: C\nla la la\n\n[Verse 1]\ngrid: G\nhumming\n",
		want:  "title: X\n\n[Verse 2]\ngrid: C\nla la la\n\n[Verse 1]\ngrid: G\nhumming\n",
		quiet: true,
	},
}

func TestCleanSongText(t *testing.T) {
	for _, tc := range cleanCases {
		t.Run(tc.name, func(t *testing.T) {
			got, notes := CleanSongText(tc.in, tc.opts)
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s\nnotes: %q", got, tc.want, notes)
			}
			if tc.quiet && len(notes) > 0 {
				t.Errorf("expected no notes, got %q", notes)
			}
			if tc.note != "" && !strings.Contains(strings.Join(notes, "\n"), tc.note) {
				t.Errorf("no note mentions %q: %q", tc.note, notes)
			}
			again, notes2 := CleanSongText(got, tc.opts)
			if again != got {
				t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", got, again)
			}
			if len(notes2) > 0 {
				t.Errorf("second pass still had notes: %q", notes2)
			}
		})
	}
}

// TestCleanSongTextNotesCarryNoLyrics checks the notes never quote a lyric.
func TestCleanSongTextNotesCarryNoLyrics(t *testing.T) {
	lyrics := []string{"walking", "quiet road", "la la", "humming"}
	for _, tc := range cleanCases {
		_, notes := CleanSongText(tc.in, tc.opts)
		for _, n := range notes {
			for _, l := range lyrics {
				if strings.Contains(n, l) {
					t.Errorf("%s: note %q quotes lyric text", tc.name, n)
				}
			}
		}
	}
}

// TestCleanSongTextMessyImport runs a whole messy draft, the shape a UG
// import produces, and checks the result parses into the expected
// sections.
func TestCleanSongTextMessyImport(t *testing.T) {
	in := `title: Placeholder Song
artist: Nobody

# imported from https://example.com/tab
[Intro]
grid: Am | F | C | G
Tabbed by: someone
Capo 2
e|---0---1---3---|
B|---1---1---0---|

[Verse 1]
grid: Am | F | C | G
Am       F
[Am]walking down the [F]quiet road
[C]humming in the [G]rain


la la la (x2)

[Chorus]
grid: C | G | Am | F
Chorus:
oh la la la
oh la la la
[x2]

[Verse 2]
grid: Am | F | C | G
[Am]counting all the [F]paper stars
[C]under neon [G]skies

[Chorus]

[Outro]
grid: |: Am | F :| Bb6 | Cmaj9 | D7sus4 | Ebdim | F#m7b5 | G9 | Abmaj7 | Bbm6 | C#7 | Dadd9 | E7#9 | Gm11 | Asus2 | Db6 | Emaj7#11 | Fm6 | Gb7 | Ab9 | B13 | Cm7 | Dm9 | Eb7 | Fsus2 | G#m | Am9 | Bbmaj7 | C7 | D9 | Em11 | F6 | Gmaj7 | Abm | Bb9 | Cm6 | Db9 | Ebmaj7 | E9 | F#7 | G#7 | A7sus4
Chords used:
****
`
	want := `title: Placeholder Song
artist: Nobody
capo: 2

# imported from https://example.com/tab

[Intro]
grid: Am | F | C | G

[Verse 1]
grid: Am | F | C | G
walking down the quiet road
humming in the rain

la la la

[Chorus]
grid: C | G | Am | F
oh la la la
oh la la la
times: 2

[Verse 2]
grid: Am | F | C | G
counting all the paper stars
under neon skies

[Chorus]

[Outro]
grid: |: Am | F :|
`
	got, notes := CleanSongText(in, CleanOptions{StripInlineChords: true})
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	for _, n := range notes {
		t.Log(n)
	}
	s := song.Parse(got)
	var labels []string
	for _, sec := range s.Sections {
		labels = append(labels, sec.Label)
	}
	if strings.Join(labels, ",") != "Intro,Verse 1,Chorus,Verse 2,Chorus,Outro" {
		t.Errorf("sections = %v", labels)
	}
	if s.Get("capo") != "2" || s.Sections[2].Times != 2 {
		t.Errorf("capo=%q chorus times=%d", s.Get("capo"), s.Sections[2].Times)
	}
	if again, _ := CleanSongText(got, CleanOptions{StripInlineChords: true}); again != got {
		t.Errorf("not idempotent:\n%s", again)
	}
}
