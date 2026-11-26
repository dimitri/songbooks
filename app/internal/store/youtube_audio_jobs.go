package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The YouTube reference's audio, fetched in the background (migration
// 0140, httpapi/yt_audio_jobs.go): one job per song, for one link. A
// reference set or changed queues it (the songs trigger); so does the
// song opened without it (EnsureYouTubeAudio), an editor's "Try again"
// or "Again" (QueueYouTubeAudio), cmd/backfillaudio.

// YouTube audio job states (song_youtube_audio_jobs.status).
const (
	YTAudioQueued  = "queued"
	YTAudioRunning = "running"
	YTAudioDone    = "done"
	YTAudioFailed  = "failed"
	YTAudioRemoved = "removed"
)

// YTAudioRetryAfterFailure: a song opened this long after its audio
// failed tries again by itself (the video may be back, the block lifted).
const YTAudioRetryAfterFailure = 24 * time.Hour

// YTAudioMaxAttempts: a fetch that fails in a way that may pass (rate
// limits, the network) is tried this many times in all, further apart
// each time (ytAudioBackoff), before it's failed for good.
const YTAudioMaxAttempts = 4

// ytAudioBackoff is the wait before the next try, after attempt n (1-based).
func ytAudioBackoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	default:
		return 20 * time.Minute
	}
}

// YouTubeAudioJob is a song's job.
type YouTubeAudioJob struct {
	VariantID   int64
	URL         string
	Status      string
	Reason      string
	Error       string
	Attempts    int
	Progress    *float64
	ETASeconds  *int
	Duration    *float64
	Trigger     string
	RequestedBy *int64
	QueuedAt    time.Time
	RunAfter    time.Time
	StartedAt   *time.Time
	HeartbeatAt *time.Time
	FinishedAt  *time.Time
}

const ytAudioJobCols = `variant_id, url, status, reason, error, attempts, progress, eta_seconds, duration_s, trigger,
	requested_by_person_id, queued_at, run_after, started_at, heartbeat_at, finished_at`

func scanYTAudioJob(row pgx.Row) (*YouTubeAudioJob, error) {
	var j YouTubeAudioJob
	var progress *float32
	err := row.Scan(&j.VariantID, &j.URL, &j.Status, &j.Reason, &j.Error, &j.Attempts, &progress, &j.ETASeconds, &j.Duration,
		&j.Trigger, &j.RequestedBy, &j.QueuedAt, &j.RunAfter, &j.StartedAt, &j.HeartbeatAt, &j.FinishedAt)
	if err != nil {
		return nil, err
	}
	if progress != nil {
		p := float64(*progress)
		j.Progress = &p
	}
	return &j, nil
}

// YouTubeAudioState is what the song page and its status API need: the
// song's reference, its job, and its YouTube audio if any.
type YouTubeAudioState struct {
	Reference string // the song's reference link, trimmed
	IsVideo   bool   // the reference is a YouTube video (is_youtube_video_url)
	Job       *YouTubeAudioJob
	// The stored audio (song_audio, category youtube): there or not, the
	// link it came from ("" uploaded), its length when known (its job's,
	// else its analysis').
	HasAudio  bool
	AudioURL  string
	AudioSize int64
	Duration  *float64
	Now       time.Time // the database's clock
}

// GetYouTubeAudioState reads a song's reference, job and YouTube audio;
// ErrNotFound for no such song.
func (st *Store) GetYouTubeAudioState(ctx context.Context, variantID int64) (*YouTubeAudioState, error) {
	var s YouTubeAudioState
	var audioID *int64
	var audioURL *string
	var audioSize *int64
	var analysisDur *float64
	err := st.db.QueryRow(ctx, `
		SELECT btrim(coalesce(s.reference_url, '')), is_youtube_video_url(s.reference_url), now(),
		       a.id, a.source_url, a.byte_size, an.duration_s
		FROM songs s
		LEFT JOIN song_audio a ON a.variant_id = s.id AND a.category = 'youtube'
		LEFT JOIN song_audio_analysis an ON an.song_audio_id = a.id AND an.source_sha256 = a.sha256
		WHERE s.id = $1`, variantID).Scan(&s.Reference, &s.IsVideo, &s.Now, &audioID, &audioURL, &audioSize, &analysisDur)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if audioID != nil {
		s.HasAudio = true
		s.AudioURL = *audioURL
		s.AudioSize = *audioSize
	}
	j, err := scanYTAudioJob(st.db.QueryRow(ctx, `SELECT `+ytAudioJobCols+` FROM song_youtube_audio_jobs WHERE variant_id = $1`, variantID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	s.Job = j
	if j != nil && j.Duration != nil && s.HasAudio && j.URL == s.AudioURL {
		s.Duration = j.Duration
	} else if s.HasAudio && analysisDur != nil {
		s.Duration = analysisDur
	}
	return &s, nil
}

// EnsureYouTubeAudio is the song opened: its YouTube reference's audio
// queued when there's none, no job under way, none removed by an editor
// for this link, and none failed in the last day. true when it queued
// one.
func (st *Store) EnsureYouTubeAudio(ctx context.Context, variantID int64) (bool, error) {
	tag, err := st.db.Exec(ctx, `
		INSERT INTO song_youtube_audio_jobs (variant_id, url, trigger)
		SELECT s.id, btrim(s.reference_url), 'open' FROM songs s
		WHERE s.id = $1 AND is_youtube_video_url(s.reference_url)
		  AND NOT EXISTS (SELECT 1 FROM song_audio a WHERE a.variant_id = s.id AND a.category = 'youtube')
		  AND NOT EXISTS (SELECT 1 FROM song_youtube_audio_jobs j WHERE j.variant_id = s.id AND j.url = btrim(s.reference_url)
		                  AND (j.status IN ('queued', 'running', 'removed')
		                       OR (j.status = 'failed' AND j.finished_at > now() - $2::float8 * interval '1 second')))
		ON CONFLICT (variant_id) DO UPDATE SET
		  url = EXCLUDED.url, status = 'queued', reason = '', error = '', attempts = 0,
		  progress = NULL, eta_seconds = NULL, duration_s = NULL, trigger = EXCLUDED.trigger,
		  requested_by_person_id = NULL, queued_at = now(), run_after = now(),
		  started_at = NULL, heartbeat_at = NULL, finished_at = NULL`,
		variantID, YTAudioRetryAfterFailure.Seconds())
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	return true, st.notifyYTAudio(ctx, variantID)
}

// QueueYouTubeAudio queues the fetch of url's audio for a song, asked
// for (an editor's "Try again" or "Again", the import wizard, the
// backfill): whatever came before for it, unless the same link's job is
// queued or running already. by is who asked (0: no one). true when it
// queued one.
func (st *Store) QueueYouTubeAudio(ctx context.Context, variantID int64, url, trigger string, by int64) (bool, error) {
	var who *int64
	if by != 0 {
		who = &by
	}
	tag, err := st.db.Exec(ctx, `
		INSERT INTO song_youtube_audio_jobs (variant_id, url, trigger, requested_by_person_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (variant_id) DO UPDATE SET
		  url = EXCLUDED.url, status = 'queued', reason = '', error = '', attempts = 0,
		  progress = NULL, eta_seconds = NULL, duration_s = NULL, trigger = EXCLUDED.trigger,
		  requested_by_person_id = EXCLUDED.requested_by_person_id, queued_at = now(), run_after = now(),
		  started_at = NULL, heartbeat_at = NULL, finished_at = NULL
		WHERE NOT (song_youtube_audio_jobs.url = EXCLUDED.url AND song_youtube_audio_jobs.status IN ('queued', 'running'))`,
		variantID, url, trigger, who)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	return true, st.notifyYTAudio(ctx, variantID)
}

func (st *Store) notifyYTAudio(ctx context.Context, variantID int64) error {
	_, err := st.db.Exec(ctx, `SELECT pg_notify('song_youtube_audio', $1::bigint::text)`, variantID)
	return err
}

// MarkYouTubeAudioRemoved: an editor removed the song's YouTube audio;
// it isn't fetched again for this link by itself (EnsureYouTubeAudio).
func (st *Store) MarkYouTubeAudioRemoved(ctx context.Context, variantID int64) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO song_youtube_audio_jobs (variant_id, url, status, trigger, finished_at)
		SELECT s.id, btrim(s.reference_url), 'removed', 'removed', now() FROM songs s
		WHERE s.id = $1 AND is_youtube_video_url(s.reference_url)
		ON CONFLICT (variant_id) DO UPDATE SET
		  url = EXCLUDED.url, status = 'removed', reason = '', error = '', progress = NULL, eta_seconds = NULL,
		  finished_at = now()`, variantID)
	return err
}

// RequeueYouTubeAudio puts back in the queue the running jobs whose
// worker went quiet for staleAfter (0: every running job, at start-up:
// this process is the only worker and has none yet).
func (st *Store) RequeueYouTubeAudio(ctx context.Context, staleAfter time.Duration) (int64, error) {
	tag, err := st.db.Exec(ctx, `
		UPDATE song_youtube_audio_jobs SET status = 'queued', progress = NULL, eta_seconds = NULL,
		       started_at = NULL, heartbeat_at = NULL, run_after = now()
		WHERE status = 'running' AND ($1::float8 <= 0 OR coalesce(heartbeat_at, started_at, queued_at) < now() - $1::float8 * interval '1 second')`,
		staleAfter.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ClaimYouTubeAudioJob takes the oldest job due, marking it running, or
// nil when there's none. Workers may run side by side (SKIP LOCKED).
func (st *Store) ClaimYouTubeAudioJob(ctx context.Context) (*YouTubeAudioJob, error) {
	j, err := scanYTAudioJob(st.db.QueryRow(ctx, `
		UPDATE song_youtube_audio_jobs SET status = 'running', attempts = attempts + 1, started_at = now(),
		       heartbeat_at = now(), progress = 0, eta_seconds = NULL
		WHERE variant_id = (
			SELECT variant_id FROM song_youtube_audio_jobs
			WHERE status = 'queued' AND run_after <= now()
			ORDER BY run_after, queued_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING `+ytAudioJobCols))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

// ytAudioClaimed: the job is still the one claimed (not replaced by
// another link, taken away, or queued again after its worker went quiet).
const ytAudioClaimed = `variant_id = $1 AND url = $2 AND status = 'running' AND started_at = $3`

// ReportYouTubeAudioProgress records a running job's progress (0..1) and
// ETA (seconds, nil unknown), and that its worker is alive; false when
// the job isn't this worker's any more.
func (st *Store) ReportYouTubeAudioProgress(ctx context.Context, j *YouTubeAudioJob, progress float64, eta *int) (bool, error) {
	tag, err := st.db.Exec(ctx, `
		UPDATE song_youtube_audio_jobs SET progress = $4, eta_seconds = $5, heartbeat_at = now()
		WHERE `+ytAudioClaimed, j.VariantID, j.URL, j.StartedAt, progress, eta)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// FinishYouTubeAudioJob stores the audio as the song's YouTube recording,
// as an upload is (song_audio: its split, then its analysis, follow), and
// marks the job done; false (nothing stored) when the job isn't this
// worker's any more (another link meanwhile, taken away).
func (st *Store) FinishYouTubeAudioJob(ctx context.Context, j *YouTubeAudioJob, data []byte, duration float64) (bool, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var one int
	err = tx.QueryRow(ctx, `SELECT 1 FROM song_youtube_audio_jobs WHERE `+ytAudioClaimed+` FOR UPDATE`, j.VariantID, j.URL, j.StartedAt).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(data)
	if _, err := tx.Exec(ctx, `
		INSERT INTO song_audio (variant_id, category, content_type, byte_size, sha256, source_url, data, created_by_person_id)
		VALUES ($1, 'youtube', 'audio/mpeg', $2, $3, $4, $5, $6)
		ON CONFLICT (variant_id, category) DO UPDATE SET
			content_type = EXCLUDED.content_type, byte_size = EXCLUDED.byte_size,
			sha256 = EXCLUDED.sha256, source_url = EXCLUDED.source_url,
			data = EXCLUDED.data, created_at = now(), created_by_person_id = EXCLUDED.created_by_person_id`,
		j.VariantID, len(data), hex.EncodeToString(sum[:]), j.URL, data, j.RequestedBy); err != nil {
		return false, err
	}
	var dur *float64
	if duration > 0 {
		dur = &duration
	}
	if _, err := tx.Exec(ctx, `
		UPDATE song_youtube_audio_jobs SET status = 'done', progress = 1, eta_seconds = NULL, reason = '', error = '',
		       duration_s = $4, finished_at = now(), heartbeat_at = now()
		WHERE `+ytAudioClaimed, j.VariantID, j.URL, j.StartedAt, dur); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// FailYouTubeAudioJob records why a fetch failed: queued again after a
// wait when it may pass (transient) and tries are left, else failed for
// good with its reason. The job's status after it ("" when the job isn't
// this worker's any more).
func (st *Store) FailYouTubeAudioJob(ctx context.Context, j *YouTubeAudioJob, reason, detail string, transient bool) (string, error) {
	status := YTAudioFailed
	wait := time.Duration(0)
	if transient && j.Attempts < YTAudioMaxAttempts {
		status, wait = YTAudioQueued, ytAudioBackoff(j.Attempts)
	}
	if len(detail) > 2000 {
		detail = detail[len(detail)-2000:]
	}
	tag, err := st.db.Exec(ctx, `
		UPDATE song_youtube_audio_jobs SET status = $4, reason = $5, error = $6, progress = NULL, eta_seconds = NULL,
		       run_after = now() + $7::float8 * interval '1 second', finished_at = now(), heartbeat_at = NULL
		WHERE `+ytAudioClaimed, j.VariantID, j.URL, j.StartedAt, status, reason, detail, wait.Seconds())
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", nil
	}
	return status, nil
}

// WaitForYouTubeAudioJob waits until a job is queued (the notification)
// or timeout passes, whichever comes first.
func (st *Store) WaitForYouTubeAudioJob(ctx context.Context, timeout time.Duration) error {
	conn, err := st.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `LISTEN song_youtube_audio`); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `UNLISTEN song_youtube_audio`)
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err = conn.Conn().WaitForNotification(wctx)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return nil
	}
	return err
}

// YouTubeAudioBackfill is cmd/backfillaudio's count: the songs with a
// YouTube video as their reference, those with its audio, and of those
// without, where their job is; Candidates the songs it would queue (no
// job, or one done or failed more than a day ago), oldest first.
type YouTubeAudioBackfill struct {
	WithVideo, WithAudio, Queued, Running, Failed, FailedRecently, Removed, Done int
	Candidates                                                                []int64
}

// CountYouTubeAudioBackfill counts what cmd/backfillaudio would queue.
func (st *Store) CountYouTubeAudioBackfill(ctx context.Context) (*YouTubeAudioBackfill, error) {
	var b YouTubeAudioBackfill
	rows, err := st.db.Query(ctx, `
		SELECT s.id, EXISTS (SELECT 1 FROM song_audio a WHERE a.variant_id = s.id AND a.category = 'youtube'),
		       coalesce(j.status, ''), coalesce(j.url = btrim(s.reference_url), false),
		       coalesce(j.status = 'failed' AND j.finished_at > now() - $1::float8 * interval '1 second', false)
		FROM songs s
		LEFT JOIN song_youtube_audio_jobs j ON j.variant_id = s.id
		WHERE is_youtube_video_url(s.reference_url)
		ORDER BY s.id`, YTAudioRetryAfterFailure.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var has, sameURL, recent bool
		var status string
		if err := rows.Scan(&id, &has, &status, &sameURL, &recent); err != nil {
			return nil, err
		}
		b.WithVideo++
		if has {
			b.WithAudio++
			continue
		}
		if !sameURL {
			status = ""
		}
		switch status {
		case YTAudioQueued:
			b.Queued++
			continue
		case YTAudioRunning:
			b.Running++
			continue
		case YTAudioRemoved:
			b.Removed++
			continue
		case YTAudioFailed:
			b.Failed++
			if recent {
				b.FailedRecently++
				continue
			}
		case YTAudioDone:
			b.Done++
		}
		b.Candidates = append(b.Candidates, id)
	}
	return &b, rows.Err()
}

// QueueYouTubeAudioBackfill queues one song's audio for the backfill, if
// it still needs it (EnsureYouTubeAudio's rule, a failure a day old
// tried again).
func (st *Store) QueueYouTubeAudioBackfill(ctx context.Context, variantID int64) (bool, error) {
	var url string
	err := st.db.QueryRow(ctx, `
		SELECT btrim(s.reference_url) FROM songs s
		WHERE s.id = $1 AND is_youtube_video_url(s.reference_url)
		  AND NOT EXISTS (SELECT 1 FROM song_audio a WHERE a.variant_id = s.id AND a.category = 'youtube')
		  AND NOT EXISTS (SELECT 1 FROM song_youtube_audio_jobs j WHERE j.variant_id = s.id AND j.url = btrim(s.reference_url)
		                  AND (j.status IN ('queued', 'running', 'removed')
		                       OR (j.status = 'failed' AND j.finished_at > now() - $2::float8 * interval '1 second')))`,
		variantID, YTAudioRetryAfterFailure.Seconds()).Scan(&url)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return st.QueueYouTubeAudio(ctx, variantID, url, "backfill", 0)
}

// YouTubeAudioJobStatus is one song's job status ("" for none), for the
// backfill to wait on.
func (st *Store) YouTubeAudioJobStatus(ctx context.Context, variantID int64) (string, error) {
	var s string
	err := st.db.QueryRow(ctx, `SELECT status FROM song_youtube_audio_jobs WHERE variant_id = $1`, variantID).Scan(&s)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return s, err
}
