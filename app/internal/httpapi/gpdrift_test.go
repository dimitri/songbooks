package httpapi

import (
	"strings"
	"testing"

	"app/internal/gpfacts"
	"app/internal/song"
	"app/internal/store"
)

func driftFacts() *gpfacts.Facts {
	return &gpfacts.Facts{
		Tempo: 100, Swing: "8", Bars: 4,
		Played:  []int{1, 2, 3, 4},
		Time:    []gpfacts.TimeSig{{Bar: 1, Num: 4, Den: 4}},
		Keys:    []gpfacts.KeySig{{Bar: 1, Sig: 4}},
		Markers: []gpfacts.Marker{{Bar: 1, Text: "Verse"}},
		Tracks:  []gpfacts.Track{{N: 1, Name: "Lead", Tuning: []int{64, 59, 55, 50, 45, 40}}},
		Chords: [][]gpfacts.BarChord{
			{{Pos: 0, Name: "E7"}}, {{Pos: 0, Name: "A7"}}, {}, {{Pos: 0, Name: "E7"}, {Pos: 0.5, Name: "B7"}},
		},
		Diagrams: []gpfacts.Diagram{{Name: "A7", Base: 1, Frets: []int{3, 2, 2, 2, 0, -1}, Barres: []int{2}}},
	}
}

func driftOf(t *testing.T, text string, kept map[string]string) map[string]driftItem {
	t.Helper()
	sg := song.Parse(text)
	f := driftFacts()
	out := map[string]driftItem{}
	for _, it := range computeDrift(sg, buildSections(withGPOnlyBars(sg, f)), f, kept) {
		out[it.Fact] = it
	}
	return out
}

func TestDriftInStep(t *testing.T) {
	got := driftOf(t, "title: X\ntempo: 100\nkey: E\nswing: 8\nchord: A7 XO(2223)\n\n[Verse]\ngp[Lead]: 1-4\ngrid: |E7 | A7 | . | E7 B7 |\n", nil)
	if len(got) != 0 {
		t.Errorf("expected no drift, got %+v", got)
	}
}

func TestDriftFindsEachKind(t *testing.T) {
	got := driftOf(t, "title: X\ntempo: 96\ntime: 3/4\nkey: A\n\n[Chorus]\ngp[Lead]: 1-4\ngrid: |E7 | A7 | . | E7 |\n", nil)
	for _, fact := range []string{"tempo", "time", "key", "swing", "diagram:a7", "marker:0", "chords:0"} {
		if _, ok := got[fact]; !ok {
			t.Errorf("missing %s in %v", fact, keys(got))
		}
	}
	if it := got["chords:0"]; it.Title != "Chords, bar 4" || it.Text != "E7" || it.File != "E7 B7" {
		t.Errorf("chords item = %+v", it)
	}
	if p := got["tempo"].Push; p == nil || p.Tempo != 96 {
		t.Errorf("tempo push = %+v", p)
	}
}

func TestDriftReferencesAndBarCount(t *testing.T) {
	got := driftOf(t, "title: X\ntempo: 100\nswing: 8\nkey: E\nchord: A7 XO(2223)\n\n[Verse]\ngp[Rhythm]: 1-4\ngp[Lead]: 1-9\ngp[Lead]: 1-2\ngrid: |E7 | A7 | . |\n", nil)
	if it := got["ref:0:0"]; !strings.Contains(it.File, `"Lead"`) {
		t.Errorf("unknown track = %+v", it)
	}
	if it := got["ref:0:1"]; it.File != "4 bars" {
		t.Errorf("bars past the end = %+v", it)
	}
	if it := got["bars:0"]; it.Text != "3 bars" || !it.Take {
		t.Errorf("bar count = %+v", it)
	}
}

func TestDriftKept(t *testing.T) {
	text := "title: X\ntempo: 96\nkey: E\nswing: 8\nchord: A7 XO(2223)\n\n[Verse]\ngp[Lead]: 1-4\ngrid: |E7 | A7 | . | E7 B7 |\n"
	first := driftOf(t, text, nil)["tempo"]
	if again := driftOf(t, text, map[string]string{"tempo": first.Sig})["tempo"]; !again.Kept {
		t.Errorf("kept on the same values: %+v", again)
	}
	moved := driftOf(t, strings.Replace(text, "tempo: 96", "tempo: 92", 1), map[string]string{"tempo": first.Sig})["tempo"]
	if moved.Kept {
		t.Error("a kept difference must come back when a value changes")
	}
}

func TestGPOnlySectionGetsBars(t *testing.T) {
	sg := song.Parse("title: X\ncontent: gp\n\n[Solo]\ngp[Lead]: 2-3\n")
	sections := buildSections(withGPOnlyBars(sg, driftFacts()))
	attachGPRefs(sections, sg, songGP{Meta: &store.SongGPSourceFile{}, Facts: driftFacts()})
	if sections[0].BarCount != 2 || !sections[0].GPOnly || len(sections[0].GP) != 1 || sections[0].GP[0].TrackIndex != 0 {
		t.Errorf("section = bars %d, gpOnly %v, gp %+v", sections[0].BarCount, sections[0].GPOnly, sections[0].GP)
	}
}

func TestFileGrid(t *testing.T) {
	ref, _ := gpfacts.ParseRef("Lead", "1-4")
	var toks []string
	for _, m := range fileGrid(driftFacts(), ref) {
		toks = append(toks, strings.Join(m.Tokens, " "))
	}
	if got := strings.Join(toks, " | "); got != "E7 | A7 | . | E7 B7" {
		t.Errorf("fileGrid = %q", got)
	}
}

func keys(m map[string]driftItem) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestWithGPNotes(t *testing.T) {
	sg := song.Parse("title: X\n\n[Verse]\nnote: softly\ngp[Lead]: 1-4\ngp: 5\ngrid: |E7 |\n")
	out := withGPNotes(sg)
	if got := strings.Join(out.Sections[0].Notes, " / "); got != "softly / Guitar Pro file: Lead, bars 1-4 / Guitar Pro file: every track, bars 5" {
		t.Errorf("notes = %q", got)
	}
	if len(sg.Sections[0].Notes) != 1 {
		t.Error("the song itself must not change")
	}
}

// A compound song's tempo counts dotted quarters, the file's quarters:
// 6/8 at 46 is the file's 69, in step; a file at 72 is 48 here, taken as
// 48 and pushed back as 72 quarters.
func TestDriftTempoCompound(t *testing.T) {
	text := "title: X\ntempo: 46\ntime: 6/8\nkey: E\nswing: 8\nchord: A7 XO(2223)\n\n[Verse]\ngp[Lead]: 1-4\ngrid: |E7 | A7 | . | E7 B7 |\n"
	f := func(tempo int) map[string]driftItem {
		sg := song.Parse(text)
		facts := driftFacts()
		facts.Tempo = tempo
		facts.Time = []gpfacts.TimeSig{{Bar: 1, Num: 6, Den: 8}}
		out := map[string]driftItem{}
		for _, it := range computeDrift(sg, buildSections(withGPOnlyBars(sg, facts)), facts, nil) {
			out[it.Fact] = it
		}
		return out
	}
	if it, ok := f(69)["tempo"]; ok {
		t.Errorf("6/8 at 46 against a file at 69 quarters: %+v", it)
	}
	it, ok := f(72)["tempo"]
	if !ok {
		t.Fatal("a file at 72 quarters (48 dotted) against 46: no difference")
	}
	if it.File != "48" || it.Text != "46" {
		t.Errorf("tempo item = %+v, want here 46, file 48", it)
	}
	if it.Push == nil || it.Push.Tempo != 69 {
		t.Errorf("push = %+v, want 69 quarters", it.Push)
	}
}
