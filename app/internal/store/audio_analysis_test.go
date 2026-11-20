package store

import (
	"context"
	"testing"

	"app/internal/chartalign"
)

// A split saved queues the recording's analysis; the worker takes it once
// the backing part is there, stores it; replacing the recording hides the
// old analysis and drops one finished late for it.
func TestAnalysisJobs(t *testing.T) {
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
	if s, _ := st.GetAnalysis(ctx, id); s.Status != "" || s.Analysis != nil {
		t.Fatalf("before the split: %+v", s)
	}
	if j, _ := st.ClaimAnalysisJob(ctx, "test"); j != nil {
		t.Fatalf("claimed before the split: %+v", j)
	}
	job, err := st.ClaimStemJob(ctx, "htdemucs", LaneAll)
	if err != nil || job == nil {
		t.Fatalf("claim stems: %v %v", job, err)
	}
	if ok, err := st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v"), "no_vocals": []byte("n")}); err != nil || !ok {
		t.Fatalf("save stems: %v %v", ok, err)
	}
	if s, _ := st.GetAnalysis(ctx, id); s.Status != "queued" {
		t.Fatalf("after the split: %+v", s)
	}
	aj, err := st.ClaimAnalysisJob(ctx, "test")
	if err != nil || aj == nil || aj.SongAudioID != id || aj.SHA256 != "sha-one" {
		t.Fatalf("claim analysis: %+v %v", aj, err)
	}
	rec := &chartalign.Recording{Analyzer: "test", Duration: 200, Tempo: 90, TimeSig: "4/4", TimeSigConf: 0.97, Beats: []float64{1, 2}, BeatPos: []int{1, 2}, Chords: []chartalign.ChordSeg{{T: 0, E: 2, C: "D:maj"}}}
	if ok, err := st.SaveAnalysis(ctx, id, aj.SHA256, rec); err != nil || !ok {
		t.Fatalf("save analysis: %v %v", ok, err)
	}
	s, err := st.GetAnalysis(ctx, id)
	if err != nil || s.Status != "done" || s.Analysis == nil || s.Analysis.Tempo != 90 || len(s.Analysis.Chords) != 1 {
		t.Fatalf("after saving: %+v %v", s, err)
	}
	// Replaced: the old analysis is no longer served; a late save is dropped.
	if _, err := st.SetSongAudio(ctx, v, AudioYouTube, []byte("two"), "audio/mpeg", "sha-two", "", me); err != nil {
		t.Fatal(err)
	}
	if s, _ := st.GetAnalysis(ctx, id); s.Analysis != nil {
		t.Fatalf("old analysis served: %+v", s)
	}
	if ok, err := st.SaveAnalysis(ctx, id, "sha-one", rec); err != nil || ok {
		t.Fatalf("late save: %v %v", ok, err)
	}
	// Re-queued by hand, and the backfill.
	if err := st.QueueAnalysisJob(ctx, id); err != nil {
		t.Fatal(err)
	}
	if n, err := st.QueueAllAnalysisJobs(ctx, ""); err != nil || n != 0 {
		t.Fatalf("backfill of a recording not split: %d %v", n, err)
	}
}
