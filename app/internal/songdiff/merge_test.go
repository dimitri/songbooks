package songdiff

import (
	"strings"
	"testing"
)

const mBase = `title: Gin Lane
artist: Mercy
tempo: 96

[Intro]
grid: | Am | Am |

[Verse]
grid: | Am | G | F | E7 |
la la

[Chorus]
grid: | F | G | C | C |
oh oh
`

func TestChartChanged(t *testing.T) {
	for _, c := range []struct {
		name, after string
		want        bool
	}{
		{"the same", mBase, false},
		{"whitespace", strings.ReplaceAll(mBase, "\n\n", "\n\n\n") + "   \n", false},
		{"a comment", strings.Replace(mBase, "[Verse]", "# slower here\n[Verse]", 1), false},
		{"the title", strings.Replace(mBase, "title: Gin Lane", "title: Gin Lane (live)", 1), false},
		{"the reference", strings.Replace(mBase, "tempo: 96", "tempo: 96\nreference: https://youtu.be/x", 1), false},
		{"the tempo", strings.Replace(mBase, "tempo: 96", "tempo: 100", 1), true},
		{"a chord", strings.Replace(mBase, "| Am | G |", "| Am | G7 |", 1), true},
		{"the lyrics", strings.Replace(mBase, "la la", "la di", 1), true},
	} {
		if got := ChartChanged(mBase, c.after); got != c.want {
			t.Errorf("%s: ChartChanged = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPlanAndMerge(t *testing.T) {
	// The version: the intro skipped, the verse's lyrics its own.
	ours := strings.Replace(strings.Replace(mBase, "[Intro]\ngrid: | Am | Am |\n\n", "", 1), "la la", "la la (ours)", 1)
	// The original: faster, a G7 in the verse, a new bridge, the chorus's
	// lyrics, and the intro's chords.
	theirs := strings.NewReplacer(
		"tempo: 96", "tempo: 104",
		"| Am | G | F | E7 |", "| Am | G7 | F | E7 |",
		"[Chorus]", "[Bridge]\ngrid: | Dm | E |\n\n[Chorus]",
		"oh oh", "oh yeah",
		"| Am | Am |", "| Am | Am7 |",
	).Replace(mBase)

	parts := Plan(mBase, ours, theirs)
	got := map[string]Part{}
	for _, p := range parts {
		got[p.Key] = p
	}
	want := map[string]Part{
		"h:tempo":    {Key: "h:tempo", Kind: "field", Name: "tempo", What: "changed"},
		"s:intro#1":  {Key: "s:intro#1", Kind: "section", Name: "Intro", What: "changed", Conflict: true}, // the version dropped it
		"s:verse#1":  {Key: "s:verse#1", Kind: "section", Name: "Verse", What: "changed", Conflict: true}, // both changed it
		"s:bridge#1": {Key: "s:bridge#1", Kind: "section", Name: "Bridge", What: "added"},
		"s:chorus#1": {Key: "s:chorus#1", Kind: "section", Name: "Chorus", What: "changed"},
	}
	if len(got) != len(want) {
		t.Fatalf("parts: %+v", parts)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("part %s: %+v, want %+v", k, got[k], w)
		}
	}

	merged := Merge(mBase, ours, theirs, func(string) bool { return true })
	for _, s := range []string{"tempo: 104", "la la (ours)", "| Am | G | F | E7 |", "[Bridge]\ngrid: | Dm | E |\n\n[Chorus]", "oh yeah"} {
		if !strings.Contains(merged, s) {
			t.Errorf("merged lacks %q:\n%s", s, merged)
		}
	}
	if strings.Contains(merged, "[Intro]") || strings.Contains(merged, "G7") {
		t.Errorf("a conflicting part was taken:\n%s", merged)
	}
	if !strings.HasPrefix(merged, "title: Gin Lane\nartist: Mercy\n") {
		t.Errorf("the header moved:\n%s", merged)
	}

	// Only the chorus.
	merged = Merge(mBase, ours, theirs, func(k string) bool { return k == "s:chorus#1" })
	if !strings.Contains(merged, "oh yeah") || strings.Contains(merged, "tempo: 104") || strings.Contains(merged, "[Bridge]") {
		t.Errorf("only the chorus taken:\n%s", merged)
	}
	// Nothing: ours as it is.
	if Merge(mBase, ours, theirs, func(string) bool { return false }) != ours {
		t.Error("taking nothing changed the version")
	}
	// Taken already: nothing left to offer.
	if p := Plan(mBase, theirs, theirs); len(p) != 0 {
		t.Errorf("the version is the original: parts %+v", p)
	}
}

func TestMergeOrderAndRemoval(t *testing.T) {
	// The original plays the chorus first and drops the intro.
	theirs := `title: Gin Lane
artist: Mercy
tempo: 96

[Chorus]
grid: | F | G | C | C |
oh oh

[Verse]
grid: | Am | G | F | E7 |
la la
`
	ours := strings.Replace(mBase, "tempo: 96", "tempo: 96\ncapo: 2", 1)
	parts := Plan(mBase, ours, theirs)
	keys := []string{}
	for _, p := range parts {
		if p.Conflict {
			t.Errorf("conflict: %+v", p)
		}
		keys = append(keys, p.Key+" "+p.What)
	}
	if strings.Join(keys, ",") != "s:intro#1 removed,order moved" {
		t.Fatalf("parts: %v", keys)
	}
	merged := Merge(mBase, ours, theirs, func(string) bool { return true })
	want := "title: Gin Lane\nartist: Mercy\ntempo: 96\ncapo: 2\n\n[Chorus]\ngrid: | F | G | C | C |\noh oh\n\n[Verse]\ngrid: | Am | G | F | E7 |\nla la\n"
	if merged != want {
		t.Errorf("merged:\n%s\nwant:\n%s", merged, want)
	}
}
