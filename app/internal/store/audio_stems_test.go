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

	job, err := st.ClaimStemJob(ctx, "htdemucs", LaneAll)
	if err != nil || job == nil || job.SongAudioID != id || job.SHA256 != "sha-one" {
		t.Fatalf("claim: %+v, %v", job, err)
	}
	if again, _ := st.ClaimStemJob(ctx, "htdemucs", LaneAll); again != nil {
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
		j, err := st.ClaimStemJob(ctx, "htdemucs", LaneAll)
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

// A recording added or replaced (no parts yet) goes before one split
// again, however long that one has waited; each lane takes its own.
func TestStemJobLanes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "singer", "Singer", "singer@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(ctx, `DELETE FROM song_audio_stem_jobs`); err != nil {
		t.Fatal(err)
	}
	add := func(title, sha string) int64 {
		v, err := st.CreateVariant(ctx, OwnerPerson, me, title, "", me)
		if err != nil {
			t.Fatal(err)
		}
		id, err := st.SetSongAudio(ctx, v, AudioYouTube, []byte(sha), "audio/mpeg", sha, "", me)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	// Split once, then queued again (a new model): it has its parts.
	old := add("Old", "sha-old")
	job, err := st.ClaimStemJob(ctx, "htdemucs", LaneAll)
	if err != nil || job == nil || job.SongAudioID != old {
		t.Fatalf("claim: %+v, %v", job, err)
	}
	if ok, err := st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v")}); err != nil || !ok {
		t.Fatalf("save: %v, %v", ok, err)
	}
	if err := st.QueueStemJob(ctx, old); err != nil {
		t.Fatal(err)
	}
	// Then an upload, queued after it.
	fresh := add("Fresh", "sha-fresh")

	if j, err := st.ClaimStemJob(ctx, "htdemucs", LaneBacklog); err != nil || j == nil || j.SongAudioID != old {
		t.Fatalf("backlog lane: %+v, %v", j, err)
	}
	if j, _ := st.ClaimStemJob(ctx, "htdemucs", LaneBacklog); j != nil {
		t.Fatalf("backlog lane took an upload: %+v", j)
	}
	if j, err := st.ClaimStemJob(ctx, "htdemucs", LaneNew); err != nil || j == nil || j.SongAudioID != fresh {
		t.Fatalf("uploads' lane: %+v, %v", j, err)
	}

	// One lane for all: the upload first.
	if err := st.ResetRunningStemJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if j, err := st.ClaimStemJob(ctx, "htdemucs", LaneAll); err != nil || j == nil || j.SongAudioID != fresh {
		t.Fatalf("first: %+v, %v", j, err)
	}
	if j, err := st.ClaimStemJob(ctx, "htdemucs", LaneAll); err != nil || j == nil || j.SongAudioID != old {
		t.Fatalf("second: %+v, %v", j, err)
	}
	if _, err := ParseStemLane("fast"); err == nil {
		t.Error(`lane "fast" accepted`)
	}
}

// The worker that split a recording claims its analysis with the parts:
// no other worker sees it queued.
func TestSaveStemsClaimAnalysis(t *testing.T) {
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
	if _, err := st.db.Exec(ctx, `DELETE FROM song_audio_stem_jobs; DELETE FROM song_audio_analysis_jobs`); err != nil {
		t.Fatal(err)
	}
	id, err := st.SetSongAudio(ctx, v, AudioYouTube, []byte("one"), "audio/mpeg", "sha-one", "", me)
	if err != nil {
		t.Fatal(err)
	}
	job, err := st.ClaimStemJob(ctx, "htdemucs", LaneNew)
	if err != nil || job == nil {
		t.Fatalf("claim: %+v, %v", job, err)
	}
	ok, aj, err := st.SaveStemsClaimAnalysis(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v"), "no_vocals": []byte("n")}, "test")
	if err != nil || !ok || aj == nil || aj.SongAudioID != id || aj.SHA256 != "sha-one" || aj.Attempts != 1 {
		t.Fatalf("save: %v, %+v, %v", ok, aj, err)
	}
	if s, _ := st.GetAnalysis(ctx, id); s.Status != "running" {
		t.Fatalf("analysis: %+v", s)
	}
	if j, _ := st.ClaimAnalysisJob(ctx, "test"); j != nil {
		t.Fatalf("claimed by another worker too: %+v", j)
	}
	// Failing inline: queued again, for the backlog to try from the parts.
	if err := st.FailAnalysisJob(ctx, id, "sha-one", "boom"); err != nil {
		t.Fatal(err)
	}
	if j, _ := st.ClaimAnalysisJob(ctx, "test"); j == nil || j.SongAudioID != id || j.Attempts != 2 {
		t.Fatalf("retry: %+v", j)
	}
}
