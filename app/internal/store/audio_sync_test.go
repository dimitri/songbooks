package store

import (
	"context"
	"testing"
)

// A sync saved as a whole replaces the markers, origins included, and
// keeps the one it replaced; one marker placed by hand is "set".
func TestReplaceAudioMarkers(t *testing.T) {
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
	// The wizard's proposal: estimates.
	auto := []AudioMarker{{SectionIndex: 0, TimeSeconds: 10.45, Origin: SyncAuto}, {SectionIndex: 1, TimeSeconds: 55.75, Origin: SyncAuto}, {SectionIndex: 2, TimeSeconds: 85.1, Origin: SyncAuto}}
	if err := st.ReplaceAudioMarkers(ctx, id, auto, me); err != nil {
		t.Fatal(err)
	}
	got, _ := st.ListAudioMarkers(ctx, id)
	if len(got) != 3 || got[0].Origin != SyncAuto || got[2].TimeSeconds != 85.1 {
		t.Fatalf("after the proposal: %+v", got)
	}
	if h, _ := st.ListSyncHistory(ctx, id); len(h) != 0 {
		t.Fatalf("history after the first save: %+v", h)
	}
	// Saving the same again changes nothing, and keeps no history.
	if err := st.ReplaceAudioMarkers(ctx, id, auto, me); err != nil {
		t.Fatal(err)
	}
	if h, _ := st.ListSyncHistory(ctx, id); len(h) != 0 {
		t.Fatalf("history after an unchanged save: %+v", h)
	}
	// The Sync sheet: two sections tapped, the third dropped.
	tapped := []AudioMarker{{SectionIndex: 0, TimeSeconds: 2.7, Origin: SyncSet}, {SectionIndex: 1, TimeSeconds: 55.75, Origin: SyncAuto}}
	if err := st.ReplaceAudioMarkers(ctx, id, tapped, me); err != nil {
		t.Fatal(err)
	}
	got, _ = st.ListAudioMarkers(ctx, id)
	if len(got) != 2 || got[0].SectionIndex != 0 || got[0].Origin != SyncSet || got[1].Origin != SyncAuto {
		t.Fatalf("after the sheet: %+v", got)
	}
	h, err := st.ListSyncHistory(ctx, id)
	if err != nil || len(h) != 1 || len(h[0].Markers) != 3 || h[0].Markers[0].Origin != SyncAuto || h[0].SavedBy != "Editor" {
		t.Fatalf("history: %+v, %v", h, err)
	}
	// A flag dragged in the Mixer: that marker is set.
	if err := st.SetAudioMarker(ctx, id, 1, 56); err != nil {
		t.Fatal(err)
	}
	got, _ = st.ListAudioMarkers(ctx, id)
	if got[1].Origin != SyncSet || got[1].TimeSeconds != 56 {
		t.Fatalf("after a drag: %+v", got)
	}
	// Guitar Pro sync points keep their origin.
	if err := st.SetGPBarSyncPoints(ctx, id, []GPBarSyncPoint{{BarIndex: 0, TimeSeconds: 1, Origin: SyncAuto}, {BarIndex: 1, TimeSeconds: 3, Origin: SyncAuto}}); err != nil {
		t.Fatal(err)
	}
	if pts, _ := st.ListGPBarSyncPoints(ctx, id); len(pts) != 2 || pts[0].Origin != SyncAuto {
		t.Fatalf("sync points: %+v", pts)
	}
	if err := st.SetGPBarSyncPoints(ctx, id, []GPBarSyncPoint{{BarIndex: 0, TimeSeconds: 1}}); err != nil {
		t.Fatal(err)
	}
	if pts, _ := st.ListGPBarSyncPoints(ctx, id); len(pts) != 1 || pts[0].Origin != SyncSet {
		t.Fatalf("sync points, set: %+v", pts)
	}
}
