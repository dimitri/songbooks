package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/chartalign"
)

// AnalysisJob is one recording waiting for its analysis (migration 0106):
// its beats, bars and chords, from its parts.
type AnalysisJob struct {
	SongAudioID int64
	SHA256      string
	Attempts    int
}

// AnalysisStatus is where a recording's analysis is: "queued",
// "running", "done", "failed" or "" (no job), and the analysis itself
// when there's one for the recording as it is now.
type AnalysisStatus struct {
	Status    string
	Error     string
	Analysis  *chartalign.Recording
	Analyzer  string
	CreatedAt time.Time
	// Since: when the job was queued (queued) or started (running);
	// Ahead: the queued analyses a worker takes before this one.
	Since time.Time
	Ahead int
}

// GetAnalysisState is a recording's analysis status alone ("done" when
// there's an analysis for the recording as it is now), without reading
// the analysis: the player asks it for every recording on every song.
func (st *Store) GetAnalysisState(ctx context.Context, songAudioID int64) (string, error) {
	var s string
	err := st.db.QueryRow(ctx, `
		SELECT CASE WHEN x.song_audio_id IS NOT NULL THEN 'done' ELSE coalesce(j.status, '') END
		FROM song_audio a
		LEFT JOIN song_audio_analysis_jobs j ON j.song_audio_id = a.id AND j.source_sha256 = a.sha256
		LEFT JOIN song_audio_analysis x ON x.song_audio_id = a.id AND x.source_sha256 = a.sha256
		WHERE a.id = $1`, songAudioID).Scan(&s)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return s, err
}

// ResetRunningAnalysisJobs puts back in the queue the analyses a worker
// was on when it stopped, at the back of it: a recording that crashes
// the worker mustn't be taken first after every restart. Its claim
// counted as an attempt; one that has used them all stays failed.
func (st *Store) ResetRunningAnalysisJobs(ctx context.Context) error {
	_, err := st.db.Exec(ctx, `
		UPDATE song_audio_analysis_jobs
		SET status = CASE WHEN attempts >= $1 THEN 'failed' ELSE 'queued' END,
		    error = 'the worker stopped during the analysis', started_at = NULL, queued_at = now()
		WHERE status = 'running'`, maxStemAttempts)
	return err
}

// StartAnalysisJob marks a recording's analysis running, for the worker
// that analyses it right after its split (the split's saving queued it):
// counted as an attempt, and not claimed by another worker meanwhile.
func (st *Store) StartAnalysisJob(ctx context.Context, songAudioID int64, sha, analyzer string) (bool, error) {
	tag, err := st.db.Exec(ctx, `
		UPDATE song_audio_analysis_jobs SET status = 'running', analyzer = $3, started_at = now(), attempts = attempts + 1
		WHERE song_audio_id = $1 AND source_sha256 = $2 AND status = 'queued'`, songAudioID, sha, analyzer)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClaimAnalysisJob takes the oldest queued analysis whose recording is
// split (its parts are what it listens to), or returns nil.
func (st *Store) ClaimAnalysisJob(ctx context.Context, analyzer string) (*AnalysisJob, error) {
	var j AnalysisJob
	err := st.db.QueryRow(ctx, `
		UPDATE song_audio_analysis_jobs SET status = 'running', analyzer = $1, started_at = now(), attempts = attempts + 1
		WHERE song_audio_id = (
			SELECT j.song_audio_id FROM song_audio_analysis_jobs j
			JOIN song_audio a ON a.id = j.song_audio_id AND a.sha256 = j.source_sha256
			WHERE j.status = 'queued' AND j.attempts < $2
			  AND EXISTS (SELECT 1 FROM song_audio_stems s WHERE s.song_audio_id = a.id AND s.source_sha256 = a.sha256 AND s.stem = 'no_vocals')
			ORDER BY j.queued_at LIMIT 1 FOR UPDATE OF j SKIP LOCKED)
		RETURNING song_audio_id, source_sha256, attempts`, analyzer, maxStemAttempts).Scan(&j.SongAudioID, &j.SHA256, &j.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// SaveAnalysis stores a recording's analysis and marks its job done,
// unless the recording was replaced meanwhile (false: dropped).
func (st *Store) SaveAnalysis(ctx context.Context, songAudioID int64, sha string, r *chartalign.Recording) (bool, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return false, err
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var cur string
	err = tx.QueryRow(ctx, `SELECT sha256 FROM song_audio WHERE id = $1 FOR SHARE`, songAudioID).Scan(&cur)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && cur != sha) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	nullable := func(v float64) *float64 {
		if v == 0 {
			return nil
		}
		return &v
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO song_audio_analysis (song_audio_id, source_sha256, analyzer, duration_s, tempo_bpm, tempo_cv, time_sig, time_sig_conf, key, beat_conf, data)
		VALUES ($1, $2, $3, $4, $5, $6, nullif($7, ''), $8, nullif($9, ''), $10, $11::jsonb)
		ON CONFLICT (song_audio_id) DO UPDATE SET
			source_sha256 = EXCLUDED.source_sha256, analyzer = EXCLUDED.analyzer, created_at = now(),
			duration_s = EXCLUDED.duration_s, tempo_bpm = EXCLUDED.tempo_bpm, tempo_cv = EXCLUDED.tempo_cv,
			time_sig = EXCLUDED.time_sig, time_sig_conf = EXCLUDED.time_sig_conf, key = EXCLUDED.key,
			beat_conf = EXCLUDED.beat_conf, data = EXCLUDED.data`,
		songAudioID, sha, r.Analyzer, r.Duration, nullable(r.Tempo), r.TempoCV, r.TimeSig, r.TimeSigConf, r.Key, r.BeatConf, string(data)); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO song_audio_analysis_jobs (song_audio_id, source_sha256, status, analyzer, finished_at)
		VALUES ($1, $2, 'done', $3, now())
		ON CONFLICT (song_audio_id) DO UPDATE SET status = 'done', error = '', analyzer = EXCLUDED.analyzer, finished_at = now()
		WHERE song_audio_analysis_jobs.source_sha256 = EXCLUDED.source_sha256`, songAudioID, sha, r.Analyzer); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// FailAnalysisJob records why an analysis failed: queued again, or
// failed for good after maxStemAttempts.
func (st *Store) FailAnalysisJob(ctx context.Context, songAudioID int64, sha, why string) error {
	_, err := st.db.Exec(ctx, `
		UPDATE song_audio_analysis_jobs
		SET status = CASE WHEN attempts >= $3 THEN 'failed' ELSE 'queued' END, error = $4, finished_at = now()
		WHERE song_audio_id = $1 AND source_sha256 = $2 AND status IN ('running', 'queued')`,
		songAudioID, sha, maxStemAttempts, why)
	return err
}

// QueueAnalysisJob queues a recording's analysis again (an editor's
// "listen again", the backfill after a model changes). Nothing happens
// to a recording that isn't split yet: its split queues it.
func (st *Store) QueueAnalysisJob(ctx context.Context, songAudioID int64) error {
	tag, err := st.db.Exec(ctx, `
		INSERT INTO song_audio_analysis_jobs (song_audio_id, source_sha256)
		SELECT id, sha256 FROM song_audio WHERE id = $1
		ON CONFLICT (song_audio_id) DO UPDATE SET
			source_sha256 = EXCLUDED.source_sha256, status = 'queued', attempts = 0,
			error = '', queued_at = now(), started_at = NULL, finished_at = NULL`, songAudioID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, err = st.db.Exec(ctx, `SELECT pg_notify('song_audio_stems', $1::bigint::text)`, songAudioID)
	return err
}

// QueueAllAnalysisJobs queues every split recording whose analysis isn't
// from analyzer (all of them when analyzer is ""): the backfill. It
// returns how many were queued.
func (st *Store) QueueAllAnalysisJobs(ctx context.Context, analyzer string) (int64, error) {
	tag, err := st.db.Exec(ctx, `
		INSERT INTO song_audio_analysis_jobs (song_audio_id, source_sha256)
		SELECT a.id, a.sha256 FROM song_audio a
		WHERE EXISTS (SELECT 1 FROM song_audio_stems s WHERE s.song_audio_id = a.id AND s.source_sha256 = a.sha256 AND s.stem = 'no_vocals')
		  AND NOT EXISTS (SELECT 1 FROM song_audio_analysis x WHERE x.song_audio_id = a.id AND x.source_sha256 = a.sha256 AND $1 <> '' AND x.analyzer = $1)
		ON CONFLICT (song_audio_id) DO UPDATE SET
			source_sha256 = EXCLUDED.source_sha256, status = 'queued', attempts = 0,
			error = '', queued_at = now(), started_at = NULL, finished_at = NULL
		WHERE song_audio_analysis_jobs.status <> 'running'`, analyzer)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() > 0 {
		_, err = st.db.Exec(ctx, `SELECT pg_notify('song_audio_stems', 'analysis')`)
	}
	return tag.RowsAffected(), err
}

// GetAnalysis is a recording's analysis, for the recording as it is now,
// and its job's state.
func (st *Store) GetAnalysis(ctx context.Context, songAudioID int64) (AnalysisStatus, error) {
	var s AnalysisStatus
	var data []byte
	var at, since *time.Time
	err := st.db.QueryRow(ctx, `
		SELECT coalesce(j.status, ''), coalesce(j.error, ''), x.data, coalesce(x.analyzer, ''), x.created_at,
			CASE j.status WHEN 'running' THEN j.started_at WHEN 'queued' THEN j.queued_at END,
			CASE WHEN j.status = 'queued' THEN (
				SELECT count(*) FROM song_audio_analysis_jobs q
				WHERE q.status IN ('queued', 'running') AND q.song_audio_id <> a.id
				  AND (q.status = 'running' OR q.queued_at < j.queued_at)) ELSE 0 END
		FROM song_audio a
		LEFT JOIN song_audio_analysis_jobs j ON j.song_audio_id = a.id AND j.source_sha256 = a.sha256
		LEFT JOIN song_audio_analysis x ON x.song_audio_id = a.id AND x.source_sha256 = a.sha256
		WHERE a.id = $1`, songAudioID).Scan(&s.Status, &s.Error, &data, &s.Analyzer, &at, &since, &s.Ahead)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	if at != nil {
		s.CreatedAt = *at
	}
	if since != nil {
		s.Since = *since
	}
	if len(data) > 0 {
		var r chartalign.Recording
		if err := json.Unmarshal(data, &r); err != nil {
			return s, err
		}
		s.Analysis = &r
	}
	return s, nil
}
