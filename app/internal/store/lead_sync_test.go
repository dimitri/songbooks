package store

import (
	"context"
	"testing"
)

// A save changing the song's "lead:" moves what's stored by bar number
// (the recordings' per-bar sync points) by the change; the section markers
// (times) stay; taking the lead off and putting it back is a round trip.
func TestLeadMovesBarSync(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "editor", "Editor", "editor@example.com")
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.CreateVariant(ctx, OwnerPerson, me, "Song", "", me)
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.SetSongAudio(ctx, v, AudioYouTube, []byte("mp3"), "audio/mpeg", "sha", "", me)
	if err != nil {
		t.Fatal(err)
	}
	pts := []GPBarSyncPoint{{BarIndex: 0, TimeSeconds: 1}, {BarIndex: 1, TimeSeconds: 3}, {BarIndex: 2, TimeSeconds: 5}}
	if err := st.ReplaceSync(ctx, id, []AudioMarker{{SectionIndex: 0, TimeSeconds: 1}}, me, &pts); err != nil {
		t.Fatal(err)
	}
	bars := func() []int {
		got, err := st.ListGPBarSyncPoints(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		var out []int
		for _, p := range got {
			out = append(out, p.BarIndex)
		}
		return out
	}
	save := func(body string) {
		if _, err := st.SaveVariantRaw(v, body, "", me); err != nil {
			t.Fatal(err)
		}
	}
	equal := func(a []int, b ...int) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	save("title: Song\n\n[Verse]\ngrid: | G | C | D |\n")
	if got := bars(); !equal(got, 0, 1, 2) {
		t.Fatalf("no lead: %v", got)
	}
	save("title: Song\nlead: 2\n\n[Verse]\ngrid: | G | C | D |\n")
	if got := bars(); !equal(got, 2, 3, 4) {
		t.Errorf("lead: 2 added: %v, want 2 3 4", got)
	}
	// The form written in the first section is the same lead: nothing moves.
	save("title: Song\n\n[Verse]\nlead: 2\ngrid: | G | C | D |\n")
	if got := bars(); !equal(got, 2, 3, 4) {
		t.Errorf("same lead, another form: %v", got)
	}
	save("title: Song\nlead: 1\n\n[Verse]\ngrid: | G | C | D |\n")
	if got := bars(); !equal(got, 1, 2, 3) {
		t.Errorf("lead 2 → 1: %v", got)
	}
	save("title: Song\n\n[Verse]\ngrid: | G | C | D |\n")
	save("title: Song\nlead: 1\n\n[Verse]\ngrid: | G | C | D |\n")
	if got := bars(); !equal(got, 1, 2, 3) {
		t.Errorf("off and on again: %v", got)
	}
	if m, _ := st.ListAudioMarkers(ctx, id); len(m) != 1 || m[0].TimeSeconds != 1 {
		t.Errorf("the section marker moved: %+v", m)
	}
}
