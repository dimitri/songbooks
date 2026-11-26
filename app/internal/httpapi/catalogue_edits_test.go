package httpapi

import (
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"app/internal/songimport"
)

// The weighing of a catalogue edit (catalogue_edits.go), the recording's
// verdicts given.
func TestJudgeCatalogueEdit(t *testing.T) {
	base := "title: Le Vent\nartist: Someone\n\n[Verse]\ngrid: C | G | Am | F\n[C]Le vent se [G]lève\n[Chorus]\ngrid: F | G | C | C\n"
	fits := func(before, after songimport.Fit) func(string) *songimport.Fit {
		return func(body string) *songimport.Fit {
			if body == base {
				return &before
			}
			return &after
		}
	}
	follows := songimport.Fit{Fit: songimport.FitFollows}
	differs := songimport.Fit{Fit: songimport.FitDiffers, Reasons: []songimport.FitReason{{Kind: "intro", Bars: 2}}}
	differsMore := songimport.Fit{Fit: songimport.FitDiffers, Reasons: []songimport.FitReason{{Kind: "intro", Bars: 2}, {Kind: "bars"}}}
	unknown := songimport.Fit{Fit: songimport.FitUnknown}
	moreBars := strings.Replace(base, "grid: F | G | C | C", "grid: F | G | C | C | C", 1)
	for _, c := range []struct {
		name     string
		after    string
		fit      func(string) *songimport.Fit
		newcomer bool
		want     []string
	}{
		{"lyrics, no analysis", strings.Replace(base, "lève", "lève, il faut", 1), nil, false, nil},
		{"chords, no analysis", strings.Replace(base, "Am | F", "Am7 | F", 1), nil, false, nil},
		{"a bar, no analysis", moreBars, nil, false, []string{"structure"}},
		{"a section renamed, no analysis", strings.Replace(base, "[Chorus]", "[Refrain]", 1), nil, false, []string{"structure"}},
		{"a bar, the fit kept", moreBars, fits(follows, follows), false, nil},
		{"a bar, the fit improved", moreBars, fits(differs, follows), false, nil},
		{"a bar, the fit worse", moreBars, fits(follows, differs), false, []string{"fit"}},
		{"more ways to differ", moreBars, fits(differs, differsMore), false, []string{"fit"}},
		{"no verdict either side", moreBars, fits(unknown, unknown), false, []string{"structure"}},
		{"the verdict lost", moreBars, fits(follows, unknown), false, []string{"fit"}},
		{"the lead-in", "lead: 1\n" + base, nil, false, []string{"lead"}},
		{"the Guitar Pro sync", strings.Replace(base, "artist: Someone\n", "artist: Someone\ngp_start: 3\n", 1), nil, false, []string{"gp"}},
		{"the key", strings.Replace(base, "artist: Someone\n", "artist: Someone\nkey: C\n", 1), nil, false, []string{"key"}},
		{"the artist (a fix: catalogueSave asks first)", strings.Replace(base, "Someone", "Someone Else", 1), nil, false, nil},
		{"a newcomer's typo", strings.Replace(base, "lève", "lèvent", 1), nil, true, []string{"newcomer"}},
	} {
		got := judgeCatalogueEdit(base, c.after, c.fit, c.newcomer).Reasons
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestChartTransposed(t *testing.T) {
	base := "[Verse]\ngrid: C | G | Am | F\n"
	for after, want := range map[string]bool{
		"[Verse]\ngrid: D | A | Bm | G\n":     true,
		"[Verse]\ngrid: C | G | Am7 | F\n":    false,
		"[Verse]\ngrid: C | G | Am | F | C\n": false,
		"[Verse]\ngrid: D | G | Am | F\n":     false,
		base:                                  false,
	} {
		if got := chartTransposed(base, after); got != want {
			t.Errorf("%q: %v, want %v", after, got, want)
		}
	}
}

func TestEditSummary(t *testing.T) {
	r := httptest.NewRequest("PUT", "/", nil)
	r.Header.Set(editSummaryHeader, url.QueryEscape("  le refrain :\n Am, pas A  "))
	if got := editSummary(r); got != "le refrain : Am, pas A" {
		t.Errorf("summary %q", got)
	}
	r.Header.Set(editSummaryHeader, url.QueryEscape(strings.Repeat("é", 300)))
	if got := []rune(editSummary(r)); len(got) != maxEditSummary {
		t.Errorf("a long summary: %d characters", len(got))
	}
}

func TestSameReference(t *testing.T) {
	if !sameReference("https://www.youtube.com/watch?v=AAAAAAAAAAA", "https://youtu.be/AAAAAAAAAAA") {
		t.Error("two links to one video")
	}
	if sameReference("https://www.youtube.com/watch?v=AAAAAAAAAAA", "https://www.youtube.com/watch?v=BBBBBBBBBBB") {
		t.Error("two videos")
	}
}

func TestSetHeaderLine(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"title: A\nreference: x\n\n[Verse]\n", "title: A\nreference: y\n\n[Verse]\n"},
		{"title: A\n\n[Verse]\nreference: not a header\n", "title: A\nreference: y\n\n[Verse]\nreference: not a header\n"},
		{"[Verse]\n", "reference: y\n[Verse]\n"},
	} {
		if got := setHeaderLine(c.in, "reference", "y"); got != c.want {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
	}
}

// The protected part of an edit put back (catalogue_suggestions.go): what
// goes live of a mixed save.
func TestWithoutProtected(t *testing.T) {
	cur := "title: Le Vent\nartist: Someone\nkey: C\n\n[Verse]\ngp[Lead]: 1-4\ngrid: C | G |\n\n[Verse]\ngrid: C | G |\n"
	mine := "title: Le Vent se lève\nartist: Someone\nkey: Am\nlead: 2\n\n[Verse]\ngp[Lead]: 2-5\ngrid: C | G7 |\n\n[Verse]\ngp[Lead]: 6-7\ngrid: C | G |\n"
	got := withoutProtected(cur, mine, []string{"key", "lead", "gp", "title"})
	want := "title: Le Vent\nartist: Someone\nkey: C\n\n[Verse]\ngp[Lead]: 1-4\ngrid: C | G7 |\n\n[Verse]\ngrid: C | G |\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if !suggestionPart([]string{"key", "title"}) || suggestionPart([]string{"key", "passerby"}) || suggestionPart(nil) {
		t.Error("suggestionPart")
	}
}

// A recording removed and put back: the header as it was, so nothing goes
// live.
func TestWithoutProtectedPutsBack(t *testing.T) {
	cur := "title: Le Vent\nartist: Someone\nreference: https://youtu.be/x\n\n[Verse]\nla\n"
	mine := "title: Le Vent\nartist: Someone\n\n[Verse]\nla\n"
	if got := withoutProtected(cur, mine, []string{"reference"}); got != cur {
		t.Errorf("got %q", got)
	}
}
