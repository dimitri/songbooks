package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"app/internal/store"
)

// TestSyncSheetAlwaysOffered: once every section's start is set by ear,
// the player's badge goes away, but the Sync sheet stays within reach of
// who may edit the song: "Sync…" in the ⋯ menu and the phone's Morceau
// sheet, the panel's Sync line with its button (2026-11-24, Wake Up
// Alone). Someone who only reads the song sees the line's state, never
// an entry to edit it.
func TestSyncSheetAlwaysOffered(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	audioID, err := st.SetSongAudio(ctx, f.BandSong, store.AudioYouTube, []byte("ID3"), "audio/mpeg", "abc", "", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	marks := []store.AudioMarker{{SectionIndex: 0, TimeSeconds: 0.5, Origin: store.SyncSet}, {SectionIndex: 1, TimeSeconds: 20, Origin: store.SyncSet}}
	if err := st.ReplaceAudioMarkers(ctx, audioID, marks, f.Core); err != nil {
		t.Fatal(err)
	}
	if code := as(t, st, f.Core).do("POST", fmt.Sprintf("/api/variants/%d/public", f.BandSong), map[string]bool{"public": true}).Code; code != http.StatusOK {
		t.Fatalf("publishing: %d", code)
	}

	// Fully synced: the player is told so (no badge then).
	w := as(t, st, f.Core).do("GET", fmt.Sprintf("/api/songs/%d/audio", f.BandSong), nil)
	var meta struct {
		Categories []struct {
			Category   string `json:"category"`
			SyncOrigin string `json:"syncOrigin"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil || len(meta.Categories) != 1 || meta.Categories[0].SyncOrigin != store.SyncSet {
		t.Fatalf("the recording's sync: %v %+v", err, meta)
	}

	editor := as(t, st, f.Core).page(fmt.Sprintf("/song/%d", f.BandSong))
	if n := strings.Count(editor, "data-sync-open"); n != 2 {
		t.Errorf("the editor's \"Sync…\" entries (⋯ menu, Morceau sheet): %d, want 2", n)
	}
	for _, want := range []string{`id="song-sync-open"`, `id="audio-line-sync"`, `id="audio-sync-state"`, `id="audio-sync-fix"`, `id="sync-sheet"`, "/static/js/sync-sheet.js"} {
		if !strings.Contains(editor, want) {
			t.Errorf("the editor's page lacks %s", want)
		}
	}

	viewer := as(t, st, f.Outsider).page(fmt.Sprintf("/song/%d", f.BandSong))
	if !strings.Contains(viewer, `data-can-edit="false"`) {
		t.Fatal("the outsider may edit the band's public song")
	}
	if !strings.Contains(viewer, `id="audio-sync-state"`) {
		t.Error("a reader doesn't see the sync's state")
	}
	for _, not := range []string{"data-sync-open", `id="sync-sheet"`, "/static/js/sync-sheet.js"} {
		if strings.Contains(viewer, not) {
			t.Errorf("a reader is offered the Sync sheet: %s", not)
		}
	}
}
