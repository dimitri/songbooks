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

// A Guitar Pro file's backing track isn't split: not queued when added,
// skipped by the worker when queued before (migration 0123), its parts
// made before kept but not served, and no "split again".
func TestNoStemsForGPRecordings(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "player", "Player", "player@example.com")
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.CreateVariant(ctx, OwnerPerson, me, "Lesson", "", me)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(ctx, `DELETE FROM song_audio_stem_jobs`); err != nil {
		t.Fatal(err)
	}
	id, err := st.SetSongAudio(ctx, v, AudioGP, []byte("backing"), "audio/mpeg", "sha-gp", "", me)
	if err != nil {
		t.Fatal(err)
	}
	var queued int
	if err := st.db.QueryRow(ctx, `SELECT count(*) FROM song_audio_stem_jobs WHERE song_audio_id = $1`, id).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("queued when added: %d, %v", queued, err)
	}
	// Queued before the migration, split before it too.
	if _, err := st.db.Exec(ctx, `INSERT INTO song_audio_stem_jobs (song_audio_id, source_sha256) VALUES ($1, 'sha-gp')`, id); err != nil {
		t.Fatal(err)
	}
	if job, err := st.ClaimStemJob(ctx, "htdemucs"); err != nil || job != nil {
		t.Fatalf("claimed a backing track: %+v, %v", job, err)
	}
	if _, err := st.db.Exec(ctx, `INSERT INTO song_audio_stems (song_audio_id, stem, source_sha256, content_type, byte_size, data) VALUES ($1, 'vocals', 'sha-gp', 'audio/mpeg', 1, 'v')`, id); err != nil {
		t.Fatal(err)
	}
	if s, err := st.GetStemStatus(ctx, id); err != nil || s.Status != "" || len(s.Stems) != 0 {
		t.Fatalf("status: %+v, %v", s, err)
	}
	if _, _, _, err := st.GetStemData(ctx, id, "vocals"); err != ErrNotFound {
		t.Fatalf("its vocals served: %v", err)
	}
	var kept int
	if err := st.db.QueryRow(ctx, `SELECT count(*) FROM song_audio_stems WHERE song_audio_id = $1`, id).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("parts kept: %d, %v", kept, err)
	}
	if err := st.QueueStemJob(ctx, id); err != ErrNotSplit {
		t.Fatalf("split again: %v", err)
	}
	// Another recording of the song is split as before.
	yt, err := st.SetSongAudio(ctx, v, AudioYouTube, []byte("yt"), "audio/mpeg", "sha-yt", "", me)
	if err != nil {
		t.Fatal(err)
	}
	if job, err := st.ClaimStemJob(ctx, "htdemucs"); err != nil || job == nil || job.SongAudioID != yt {
		t.Fatalf("claim the YouTube recording: %+v, %v", job, err)
	}
}
