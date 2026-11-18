package store

import (
	"context"
	"testing"
)

// A recording added is queued for its split; the worker takes it, stores
// its parts; replacing the recording queues it again and hides the old
// parts, and a split finished for the old recording is dropped.
func TestStemJobs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "singer", "Singer", "singer@example.com")
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.CreateVariant(ctx, OwnerPerson, me, "Song", "", me)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(ctx, `DELETE FROM song_audio_stem_jobs`); err != nil {
		t.Fatal(err)
	}
	id, err := st.SetSongAudio(ctx, v, AudioYouTube, []byte("one"), "audio/mpeg", "sha-one", "", me)
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := st.GetStemStatus(ctx, id); s.Status != "queued" || len(s.Stems) != 0 {
		t.Fatalf("after adding: %+v", s)
	}

	job, err := st.ClaimStemJob(ctx, "htdemucs")
	if err != nil || job == nil || job.SongAudioID != id || job.SHA256 != "sha-one" {
		t.Fatalf("claim: %+v, %v", job, err)
	}
	if again, _ := st.ClaimStemJob(ctx, "htdemucs"); again != nil {
		t.Fatalf("claimed twice: %+v", again)
	}
	ok, err := st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v"), "no_vocals": []byte("n")})
	if err != nil || !ok {
		t.Fatalf("save: %v, %v", ok, err)
	}
	if s, _ := st.GetStemStatus(ctx, id); s.Status != "done" || len(s.Stems) != 2 {
		t.Fatalf("after saving: %+v", s)
	}
	if data, _, _, err := st.GetStemData(ctx, id, "vocals"); err != nil || string(data) != "v" {
		t.Fatalf("vocals: %q, %v", data, err)
	}

	// Replaced: queued again, the old parts no longer served.
	if _, err := st.SetSongAudio(ctx, v, AudioYouTube, []byte("two"), "audio/mpeg", "sha-two", "", me); err != nil {
		t.Fatal(err)
	}
	if s, _ := st.GetStemStatus(ctx, id); s.Status != "queued" || len(s.Stems) != 0 {
		t.Fatalf("after replacing: %+v", s)
	}
	if _, _, _, err := st.GetStemData(ctx, id, "vocals"); err != ErrNotFound {
		t.Fatalf("old vocals served: %v", err)
	}
	// A split of the old recording, finishing late, is dropped.
	if ok, err := st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("old")}); err != nil || ok {
		t.Fatalf("late save: %v, %v", ok, err)
	}

	// Failing: back in the queue, then failed for good.
	for i := 1; i <= maxStemAttempts; i++ {
		j, err := st.ClaimStemJob(ctx, "htdemucs")
		if err != nil || j == nil {
			t.Fatalf("claim %d: %+v, %v", i, j, err)
		}
		if err := st.FailStemJob(ctx, *j, "boom"); err != nil {
			t.Fatal(err)
		}
	}
	if s, _ := st.GetStemStatus(ctx, id); s.Status != "failed" {
		t.Fatalf("after %d failures: %+v", maxStemAttempts, s)
	}
	if err := st.QueueStemJob(ctx, id); err != nil {
		t.Fatal(err)
	}
	if s, _ := st.GetStemStatus(ctx, id); s.Status != "queued" {
		t.Fatalf("queued again: %+v", s)
	}
}
