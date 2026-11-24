package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Stems are a reference recording's parts, split by Demucs in the
// background (cmd/stemsd, migration 0078): what the practice player's
// voice control and the Mixer's lanes play. A Guitar Pro file's backing
// track (AudioGP) isn't split (migration 0123): none queued, none served.
var Stems = []string{"vocals", "drums", "bass", "other", "no_vocals"}

// ErrNotSplit: a recording that isn't split into parts (a Guitar Pro
// file's backing track).
var ErrNotSplit = errors.New("a Guitar Pro backing track isn't split into parts")

// StemJob is one recording waiting to be split.
type StemJob struct {
	SongAudioID int64
	SHA256      string
	Attempts    int
	// Parts to keep: "all", or "voice" (vocals and no_vocals only) for a
	// song of a coaching community, all a singer plays.
	Parts string
}

// Keep is whether the job keeps the part named stem.
func (j StemJob) Keep(stem string) bool {
	return j.Parts != "voice" || stem == "vocals" || stem == "no_vocals"
}

// StemStatus is where a recording's split is: "queued", "running",
// "done", "failed", or "" (no job).
type StemStatus struct {
	Status string
	Stems  []string // the parts there are, for the recording as it is now
}

// maxStemAttempts: a recording that fails this many times stays failed.
const maxStemAttempts = 3

// ResetRunningStemJobs puts back in the queue the jobs a worker was on
// when it stopped (crashed, restarted for a deploy).
func (st *Store) ResetRunningStemJobs(ctx context.Context) error {
	_, err := st.db.Exec(ctx, `UPDATE song_audio_stem_jobs SET status = 'queued', started_at = NULL WHERE status = 'running'`)
	return err
}

// ClaimStemJob takes the oldest queued job, marking it running, or
// returns nil when there's none. Workers may run side by side: each job
// goes to one (SKIP LOCKED).
func (st *Store) ClaimStemJob(ctx context.Context, model string) (*StemJob, error) {
	var j StemJob
	// The worker looking: at work (the Melody view tells a server
	// without one, migration 0134).
	if err := st.NoteStemWorker(ctx, model); err != nil {
		return nil, err
	}
	err := st.db.QueryRow(ctx, `
		UPDATE song_audio_stem_jobs SET status = 'running', model = $1, started_at = now(), attempts = attempts + 1
		WHERE song_audio_id = (
			SELECT j.song_audio_id FROM song_audio_stem_jobs j
			JOIN song_audio a ON a.id = j.song_audio_id AND a.category <> 'gp'
			WHERE j.status = 'queued'
			ORDER BY j.queued_at LIMIT 1 FOR UPDATE OF j SKIP LOCKED)
		RETURNING song_audio_id, source_sha256, attempts,
			CASE WHEN EXISTS (
				SELECT 1 FROM song_audio a
				JOIN songbook_entries se ON se.variant_id = a.variant_id
				JOIN songbooks sb ON sb.id = se.songbook_id
				JOIN communities c ON c.id = sb.owner_community_id
				WHERE a.id = song_audio_stem_jobs.song_audio_id AND c.kind = 'coaching')
			THEN 'voice' ELSE 'all' END`, model).Scan(&j.SongAudioID, &j.SHA256, &j.Attempts, &j.Parts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// SaveStems stores a job's parts and marks it done, unless the recording
// was replaced while it ran (its new job is queued already): then the
// parts are dropped and false returned.
func (st *Store) SaveStems(ctx context.Context, job StemJob, contentType string, stems map[string][]byte) (bool, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var sha string
	err = tx.QueryRow(ctx, `SELECT sha256 FROM song_audio WHERE id = $1 FOR SHARE`, job.SongAudioID).Scan(&sha)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && sha != job.SHA256) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM song_audio_stems WHERE song_audio_id = $1`, job.SongAudioID); err != nil {
		return false, err
	}
	for _, name := range Stems {
		data, ok := stems[name]
		if !ok || !job.Keep(name) {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO song_audio_stems (song_audio_id, stem, source_sha256, content_type, byte_size, data)
			VALUES ($1, $2, $3, $4, $5, $6)`, job.SongAudioID, name, job.SHA256, contentType, len(data), data); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE song_audio_stem_jobs SET status = 'done', error = '', finished_at = now()
		WHERE song_audio_id = $1 AND source_sha256 = $2 AND status = 'running'`, job.SongAudioID, job.SHA256); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// FailStemJob records why a job failed: queued again for another try,
// or failed for good after maxStemAttempts.
func (st *Store) FailStemJob(ctx context.Context, job StemJob, why string) error {
	_, err := st.db.Exec(ctx, `
		UPDATE song_audio_stem_jobs
		SET status = CASE WHEN attempts >= $3 THEN 'failed' ELSE 'queued' END,
		    error = $4, finished_at = now()
		WHERE song_audio_id = $1 AND source_sha256 = $2 AND status = 'running'`,
		job.SongAudioID, job.SHA256, maxStemAttempts, why)
	return err
}

// GetStemStatus is a recording's split: its job's status and the parts
// stored for the recording as it is now; none for a Guitar Pro backing
// track (its parts made before migration 0123 are kept, not served).
func (st *Store) GetStemStatus(ctx context.Context, songAudioID int64) (StemStatus, error) {
	var s StemStatus
	err := st.db.QueryRow(ctx, `
		SELECT CASE WHEN a.category = 'gp' THEN '' ELSE coalesce((SELECT status FROM song_audio_stem_jobs WHERE song_audio_id = a.id), '') END,
		       CASE WHEN a.category = 'gp' THEN '{}' ELSE coalesce((SELECT array_agg(st.stem ORDER BY st.stem) FROM song_audio_stems st
		                 WHERE st.song_audio_id = a.id AND st.source_sha256 = a.sha256), '{}') END
		FROM song_audio a WHERE a.id = $1`, songAudioID).Scan(&s.Status, &s.Stems)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

// GetStemData loads one part's bytes, for the recording as it is now.
func (st *Store) GetStemData(ctx context.Context, songAudioID int64, stem string) ([]byte, string, time.Time, error) {
	var data []byte
	var contentType string
	var at time.Time
	err := st.db.QueryRow(ctx, `
		SELECT st.data, st.content_type, st.created_at FROM song_audio_stems st
		JOIN song_audio a ON a.id = st.song_audio_id AND a.sha256 = st.source_sha256 AND a.category <> 'gp'
		WHERE st.song_audio_id = $1 AND st.stem = $2`, songAudioID, stem).Scan(&data, &contentType, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", at, ErrNotFound
	}
	return data, contentType, at, err
}

// QueueStemJob queues a recording's split again (an editor's "split
// again", or a failed one retried); ErrNotSplit for a Guitar Pro backing
// track.
func (st *Store) QueueStemJob(ctx context.Context, songAudioID int64) error {
	var category string
	if err := st.db.QueryRow(ctx, `SELECT category FROM song_audio WHERE id = $1`, songAudioID).Scan(&category); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if AudioCategory(category) == AudioGP {
		return ErrNotSplit
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO song_audio_stem_jobs (song_audio_id, source_sha256)
		SELECT id, sha256 FROM song_audio WHERE id = $1
		ON CONFLICT (song_audio_id) DO UPDATE SET
			source_sha256 = EXCLUDED.source_sha256, status = 'queued', attempts = 0,
			error = '', queued_at = now(), started_at = NULL, finished_at = NULL`, songAudioID)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(ctx, `SELECT pg_notify('song_audio_stems', $1::bigint::text)`, songAudioID)
	return err
}

// WaitForStemJob waits until a recording is queued (the trigger's
// notification) or timeout passes, whichever comes first.
func (st *Store) WaitForStemJob(ctx context.Context, timeout time.Duration) error {
	conn, err := st.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `LISTEN song_audio_stems`); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `UNLISTEN song_audio_stems`)
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err = conn.Conn().WaitForNotification(wctx)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return nil
	}
	return err
}
