package store

import (
	"context"
	"testing"
	"time"
)

// The YouTube reference's audio, fetched in the background (migration
// 0140): queued when the reference is set or changed, however; one job
// per song, deduped; claimed, its progress, stored as an upload is (its
// split queued after it); failures that may pass tried again later,
// others failed for good with why; a worker gone, its job queued again;
// the song opened without it, queued, but not again within a day of a
// failure, nor after an editor removed it.
func TestYouTubeAudioJobs(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	owner, err := st.CreatePerson(ctx, "Yt Owner", "yt@example.com")
	must(err)
	song, err := st.CreateVariant(ctx, OwnerPerson, owner, "Tune", "", owner)
	must(err)
	const linkA = "https://www.youtube.com/watch?v=AAAAAAAAAAA"
	const linkB = "https://youtu.be/BBBBBBBBBBB"
	setRef := func(link string) {
		t.Helper()
		body := "title: Tune\n"
		if link != "" {
			body += "reference: " + link + "\n"
		}
		_, err := st.SaveVariantByID(ctx, song, "Tune", "", link, body+"\n[Verse]\nG C\n", "", owner)
		must(err)
	}
	job := func() *YouTubeAudioJob {
		t.Helper()
		s, err := st.GetYouTubeAudioState(ctx, song)
		must(err)
		return s.Job
	}

	// No reference: nothing. A page link that isn't a video: nothing.
	if j := job(); j != nil {
		t.Fatalf("a job without a reference: %+v", j)
	}
	setRef("https://example.com/some-page")
	if j := job(); j != nil {
		t.Fatalf("a job for a page that isn't a video: %+v", j)
	}

	// Set: queued, by the trigger. Saved again, the same link: still the
	// one job (deduped).
	setRef(linkA)
	j := job()
	if j == nil || j.Status != YTAudioQueued || j.URL != linkA || j.Trigger != "reference" {
		t.Fatalf("set: %+v", j)
	}
	queuedAt := j.QueuedAt
	setRef(linkA)
	if q, err := st.QueueYouTubeAudio(ctx, song, linkA, "retry", owner); err != nil || q {
		t.Fatalf("queued twice: %v %v", q, err)
	}
	if j := job(); !j.QueuedAt.Equal(queuedAt) {
		t.Fatalf("the job was queued again: %+v", j)
	}
	if ok, err := st.EnsureYouTubeAudio(ctx, song); err != nil || ok {
		t.Fatalf("opened while queued: %v %v", ok, err)
	}

	// Claimed: running, with progress; another worker finds nothing.
	c, err := st.ClaimYouTubeAudioJob(ctx)
	must(err)
	if c == nil || c.VariantID != song || c.Attempts != 1 || c.StartedAt == nil {
		t.Fatalf("claimed: %+v", c)
	}
	if other, err := st.ClaimYouTubeAudioJob(ctx); err != nil || other != nil {
		t.Fatalf("claimed twice: %+v %v", other, err)
	}
	eta := 12
	if ok, err := st.ReportYouTubeAudioProgress(ctx, c, 0.42, &eta); err != nil || !ok {
		t.Fatalf("progress: %v %v", ok, err)
	}
	if j := job(); j.Status != YTAudioRunning || j.Progress == nil || *j.Progress < 0.41 || *j.Progress > 0.43 || j.ETASeconds == nil || *j.ETASeconds != 12 {
		t.Fatalf("running: %+v", j)
	}

	// The link changed while it ran: the job is the new link's, the old
	// fetch's audio dropped.
	setRef(linkB)
	if j := job(); j.Status != YTAudioQueued || j.URL != linkB {
		t.Fatalf("changed: %+v", j)
	}
	if ok, err := st.ReportYouTubeAudioProgress(ctx, c, 0.5, nil); err != nil || ok {
		t.Fatalf("a stale worker reports: %v %v", ok, err)
	}
	if stored, err := st.FinishYouTubeAudioJob(ctx, c, []byte("old"), 100); err != nil || stored {
		t.Fatalf("a stale worker stores: %v %v", stored, err)
	}

	// The new one: a failure that may pass is tried again later, up to
	// YTAudioMaxAttempts, then failed for good.
	for attempt := 1; attempt <= YTAudioMaxAttempts; attempt++ {
		_, err := st.db.Exec(ctx, `UPDATE song_youtube_audio_jobs SET run_after = now() WHERE variant_id = $1`, song)
		must(err)
		c, err = st.ClaimYouTubeAudioJob(ctx)
		must(err)
		if c == nil || c.Attempts != attempt {
			t.Fatalf("attempt %d: %+v", attempt, c)
		}
		status, err := st.FailYouTubeAudioJob(ctx, c, "blocked", "HTTP Error 429", true)
		must(err)
		want := YTAudioQueued
		if attempt == YTAudioMaxAttempts {
			want = YTAudioFailed
		}
		if status != want {
			t.Fatalf("attempt %d: %s, want %s", attempt, status, want)
		}
		if want == YTAudioQueued {
			j := job()
			if !j.RunAfter.After(time.Now().Add(30*time.Second)) || j.Reason != "blocked" {
				t.Fatalf("attempt %d: not waiting before the next: %+v", attempt, j)
			}
			if again, err := st.ClaimYouTubeAudioJob(ctx); err != nil || again != nil {
				t.Fatalf("claimed before its time: %+v %v", again, err)
			}
		}
	}
	if j := job(); j.Status != YTAudioFailed || j.Reason != "blocked" || j.Error != "HTTP Error 429" {
		t.Fatalf("failed for good: %+v", j)
	}

	// Opened within a day of the failure: not again. A day later: again.
	if ok, err := st.EnsureYouTubeAudio(ctx, song); err != nil || ok {
		t.Fatalf("opened after a failure: %v %v", ok, err)
	}
	_, err = st.db.Exec(ctx, `UPDATE song_youtube_audio_jobs SET finished_at = now() - interval '25 hours' WHERE variant_id = $1`, song)
	must(err)
	if ok, err := st.EnsureYouTubeAudio(ctx, song); err != nil || !ok {
		t.Fatalf("opened a day after a failure: %v %v", ok, err)
	}
	if j := job(); j.Status != YTAudioQueued || j.Trigger != "open" || j.Attempts != 0 {
		t.Fatalf("queued on open: %+v", j)
	}

	// A failure that won't pass: failed at once.
	c, err = st.ClaimYouTubeAudioJob(ctx)
	must(err)
	if status, err := st.FailYouTubeAudioJob(ctx, c, "unavailable", "Private video", false); err != nil || status != YTAudioFailed {
		t.Fatalf("unavailable: %s %v", status, err)
	}

	// An editor's Try again: queued, whatever came before.
	if q, err := st.QueueYouTubeAudio(ctx, song, linkB, "retry", owner); err != nil || !q {
		t.Fatalf("try again: %v %v", q, err)
	}
	// The app stopped while it ran: queued again at start-up; a worker
	// gone quiet, after a while.
	c, err = st.ClaimYouTubeAudioJob(ctx)
	must(err)
	if n, err := st.RequeueYouTubeAudio(ctx, time.Hour); err != nil || n != 0 {
		t.Fatalf("a live worker's job requeued: %d %v", n, err)
	}
	_, err = st.db.Exec(ctx, `UPDATE song_youtube_audio_jobs SET heartbeat_at = now() - interval '10 minutes' WHERE variant_id = $1`, song)
	must(err)
	if n, err := st.RequeueYouTubeAudio(ctx, 3*time.Minute); err != nil || n != 1 {
		t.Fatalf("a quiet worker's job: %d %v", n, err)
	}
	c, err = st.ClaimYouTubeAudioJob(ctx)
	must(err)
	if n, err := st.RequeueYouTubeAudio(ctx, 0); err != nil || n != 1 {
		t.Fatalf("at start-up: %d %v", n, err)
	}
	if stored, err := st.FinishYouTubeAudioJob(ctx, c, []byte("x"), 1); err != nil || stored {
		t.Fatalf("the job before the restart stores: %v %v", stored, err)
	}

	// Done: stored as the song's YouTube recording, its split queued.
	c, err = st.ClaimYouTubeAudioJob(ctx)
	must(err)
	if c.RequestedBy == nil || *c.RequestedBy != owner {
		t.Fatalf("who asked: %+v", c.RequestedBy)
	}
	if stored, err := st.FinishYouTubeAudioJob(ctx, c, []byte("ID3 audio"), 222); err != nil || !stored {
		t.Fatalf("done: %v %v", stored, err)
	}
	s, err := st.GetYouTubeAudioState(ctx, song)
	must(err)
	if !s.HasAudio || s.AudioURL != linkB || s.Duration == nil || *s.Duration != 222 || s.Job.Status != YTAudioDone {
		t.Fatalf("stored: %+v %+v", s, s.Job)
	}
	meta, err := st.GetSongAudioMeta(ctx, song, AudioYouTube)
	must(err)
	if ss, err := st.GetStemStatus(ctx, meta.ID); err != nil || ss.Status != "queued" {
		t.Fatalf("its split: %+v %v", ss, err)
	}
	// Saved again with the same link: nothing to fetch.
	setRef(linkA)
	setRef(linkB)
	if j := job(); j != nil && j.URL != linkB {
		t.Fatalf("back to the link whose audio is there: another link's job stays: %+v", j)
	}

	// Removed by an editor: not fetched again on open.
	must(st.DeleteSongAudio(ctx, song, AudioYouTube))
	must(st.MarkYouTubeAudioRemoved(ctx, song))
	if ok, err := st.EnsureYouTubeAudio(ctx, song); err != nil || ok {
		t.Fatalf("opened after removal: %v %v", ok, err)
	}
	// The reference taken away: the job goes.
	setRef("")
	if j := job(); j != nil {
		t.Fatalf("no reference, a job: %+v", j)
	}
}

// cmd/backfillaudio's count: the songs with a video, with its audio,
// with a job, and those it would queue.
func TestYouTubeAudioBackfill(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	owner, err := st.CreatePerson(ctx, "Bf Owner", "bf@example.com")
	if err != nil {
		t.Fatal(err)
	}
	mk := func(title, link string) int64 {
		t.Helper()
		id, err := st.CreateVariant(ctx, OwnerPerson, owner, title, "", owner)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.SaveVariantByID(ctx, id, title, "", link, "title: "+title+"\nreference: "+link+"\n", "", owner); err != nil {
			t.Fatal(err)
		}
		return id
	}
	withAudio := mk("With audio", "https://www.youtube.com/watch?v=AAAAAAAAAAA")
	if _, err := st.SetSongAudio(ctx, withAudio, AudioYouTube, []byte("a"), "audio/mpeg", "s1", "https://www.youtube.com/watch?v=AAAAAAAAAAA", owner); err != nil {
		t.Fatal(err)
	}
	failed := mk("Failed", "https://www.youtube.com/watch?v=FFFFFFFFFFF")
	waiting := mk("Waiting", "https://www.youtube.com/watch?v=WWWWWWWWWWW")
	mk("Page", "https://example.com/page")
	// The songs there before the trigger: no job.
	if _, err := st.db.Exec(ctx, `DELETE FROM song_youtube_audio_jobs WHERE variant_id = $1`, waiting); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(ctx, `UPDATE song_youtube_audio_jobs SET status = 'failed', reason = 'unavailable', finished_at = now() WHERE variant_id = $1`, failed); err != nil {
		t.Fatal(err)
	}
	b, err := st.CountYouTubeAudioBackfill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if b.WithVideo != 3 || b.WithAudio != 1 || b.Failed != 1 || b.FailedRecently != 1 || len(b.Candidates) != 1 || b.Candidates[0] != waiting {
		t.Fatalf("count: %+v", b)
	}
	if q, err := st.QueueYouTubeAudioBackfill(ctx, waiting); err != nil || !q {
		t.Fatalf("queue: %v %v", q, err)
	}
	if q, err := st.QueueYouTubeAudioBackfill(ctx, waiting); err != nil || q {
		t.Fatalf("queued twice: %v %v", q, err)
	}
	if s, err := st.YouTubeAudioJobStatus(ctx, waiting); err != nil || s != YTAudioQueued {
		t.Fatalf("status: %q %v", s, err)
	}
}
