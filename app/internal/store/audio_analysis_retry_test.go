package store

import (
	"context"
	"testing"
)

// A recording whose analysis crashes the worker is claimed again after
// each restart only until its attempts are used, and goes to the back of
// the queue meanwhile; one queued behind another says so.
func TestAnalysisJobRetryCap(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "singer", "Singer", "singer@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(ctx, `DELETE FROM song_audio_stem_jobs; DELETE FROM song_audio_analysis_jobs`); err != nil {
		t.Fatal(err)
	}
	split := func(sha string) int64 {
		v, err := st.CreateVariant(ctx, OwnerPerson, me, "Song "+sha, "", me)
		if err != nil {
			t.Fatal(err)
		}
		id, err := st.SetSongAudio(ctx, v, AudioYouTube, []byte(sha), "audio/mpeg", sha, "", me)
		if err != nil {
			t.Fatal(err)
		}
		job, err := st.ClaimStemJob(ctx, "htdemucs")
		if err != nil || job == nil {
			t.Fatalf("claim stems: %v %v", job, err)
		}
		if ok, err := st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v"), "no_vocals": []byte("n")}); err != nil || !ok {
			t.Fatalf("save stems: %v %v", ok, err)
		}
		return id
	}
	crash := split("sha-crash")
	other := split("sha-other")
	if s, _ := st.GetAnalysis(ctx, other); s.Status != "queued" || s.Ahead != 1 || s.Since.IsZero() {
		t.Fatalf("second in the queue: %+v", s)
	}
	if s, _ := st.GetAnalysisState(ctx, other); s != "queued" {
		t.Fatalf("state %q", s)
	}
	for i := 1; i <= maxStemAttempts; i++ {
		j, err := st.ClaimAnalysisJob(ctx, "test")
		if err != nil || j == nil || j.SongAudioID != crash {
			t.Fatalf("restart %d: claimed %+v %v, want the crashing one first", i, j, err)
		}
		// The worker dies; at its restart the job is put back.
		if err := st.ResetRunningAnalysisJobs(ctx); err != nil {
			t.Fatal(err)
		}
		// Back of the queue: the other one comes first now.
		if i < maxStemAttempts {
			j2, _ := st.ClaimAnalysisJob(ctx, "test")
			if j2 == nil || j2.SongAudioID != other {
				t.Fatalf("after restart %d: %+v, want the other recording", i, j2)
			}
			if _, err := st.db.Exec(ctx, `UPDATE song_audio_analysis_jobs SET status = 'queued', attempts = 0, queued_at = now() WHERE song_audio_id = $1`, other); err != nil {
				t.Fatal(err)
			}
			if _, err := st.db.Exec(ctx, `UPDATE song_audio_analysis_jobs SET queued_at = now() - interval '1 hour' WHERE song_audio_id = $1`, crash); err != nil {
				t.Fatal(err)
			}
		}
	}
	if s, _ := st.GetAnalysis(ctx, crash); s.Status != "failed" || s.Error == "" {
		t.Fatalf("after %d crashes: %+v", maxStemAttempts, s)
	}
	// The worker analysing right after a split takes the job as running.
	if ok, err := st.StartAnalysisJob(ctx, other, "sha-other", "test"); err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	if j, _ := st.ClaimAnalysisJob(ctx, "test"); j != nil {
		t.Fatalf("claimed while another worker runs it: %+v", j)
	}
}

// Markers and per-bar points saved together, or neither.
func TestReplaceSync(t *testing.T) {
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
	pts := []GPBarSyncPoint{{BarIndex: 0, TimeSeconds: 1, Origin: SyncAuto}, {BarIndex: 1, TimeSeconds: 3, Origin: SyncAuto}}
	marks := []AudioMarker{{SectionIndex: 0, TimeSeconds: 1, Origin: SyncAuto}}
	if err := st.ReplaceSync(ctx, id, marks, me, &pts); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.ListGPBarSyncPoints(ctx, id); len(p) != 2 {
		t.Fatalf("points %+v", p)
	}
	// nil: the points stay; an empty list: dropped.
	if err := st.ReplaceSync(ctx, id, nil, me, nil); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.ListGPBarSyncPoints(ctx, id); len(p) != 2 {
		t.Fatalf("points dropped by a markers-only save: %+v", p)
	}
	none := []GPBarSyncPoint{}
	if err := st.ReplaceSync(ctx, id, nil, me, &none); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.ListGPBarSyncPoints(ctx, id); len(p) != 0 {
		t.Fatalf("points kept: %+v", p)
	}
}
