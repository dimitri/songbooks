package store

import (
	"context"
	"strings"
	"testing"
)

// TestSongGPTrackPrograms: the instruments chosen for a song's file are
// read with its meta, stay across a new version of the file, and clear.
func TestSongGPTrackPrograms(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "Guitarist", "guitarist@example.com")
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.CreateVariant(ctx, OwnerPerson, me, "A song", "", me)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := st.SetSongGPTrackPrograms(ctx, v, []byte(`[{"track":0,"name":"Gtr","program":25}]`)); err != nil || ok {
		t.Fatalf("no file yet: ok=%v err=%v", ok, err)
	}
	if err := st.SetSongGPSourceFile(ctx, v, "a.gp", "application/octet-stream", []byte("one"), "sha-one", me, nil); err != nil {
		t.Fatal(err)
	}
	if ok, err := st.SetSongGPTrackPrograms(ctx, v, []byte(`[{"track":0,"name":"Gtr","program":25}]`)); err != nil || !ok {
		t.Fatalf("set: ok=%v err=%v", ok, err)
	}
	meta, err := st.GetSongGPSourceFileMeta(ctx, v)
	if err != nil || !strings.Contains(string(meta.TrackPrograms), `"program": 25`) {
		t.Fatalf("read back: %s %v", meta.TrackPrograms, err)
	}
	if err := st.SetSongGPSourceFile(ctx, v, "a.gp", "application/octet-stream", []byte("two"), "sha-two", me, nil); err != nil {
		t.Fatal(err)
	}
	if meta, _ = st.GetSongGPSourceFileMeta(ctx, v); meta.SHA256 != "sha-two" || len(meta.TrackPrograms) == 0 {
		t.Fatalf("kept across a new version: %s %s", meta.SHA256, meta.TrackPrograms)
	}
	if ok, err := st.SetSongGPTrackPrograms(ctx, v, []byte(`[]`)); err != nil || !ok {
		t.Fatalf("clear: ok=%v err=%v", ok, err)
	}
	if meta, _ = st.GetSongGPSourceFileMeta(ctx, v); meta.TrackPrograms != nil {
		t.Fatalf("cleared: %s", meta.TrackPrograms)
	}
}
