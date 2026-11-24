package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Where a song's melody stands while its Melody view can't show it yet
// (migration 0134, docs/melody-sheet.md "Until it's ready"): the
// recording's voice separation (its stems job: queued, running, failed;
// the worker's progress), the melody's notes being found, the worker
// seen at all.

// StemWorkerFresh: a worker seen within this long is at work; none seen
// for longer and this server splits nothing (staging) — or its worker is
// down, which the page can't tell apart and says the same way.
const StemWorkerFresh = 15 * time.Minute

// NoteStemWorker notes that the worker called name is at work (it looks
// for a job at least once a minute).
func (st *Store) NoteStemWorker(ctx context.Context, name string) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO stem_workers (name, seen_at) VALUES ($1, now())
		ON CONFLICT (name) DO UPDATE SET seen_at = now()`, name)
	return err
}

// SetStemJobProgress keeps how far a running split is (0..1), as the
// worker reads it in Demucs's output.
func (st *Store) SetStemJobProgress(ctx context.Context, job StemJob, progress float64) error {
	_, err := st.db.Exec(ctx, `
		UPDATE song_audio_stem_jobs SET progress = $3, progress_at = now()
		WHERE song_audio_id = $1 AND source_sha256 = $2 AND status = 'running'`,
		job.SongAudioID, job.SHA256, max(0, min(1, progress)))
	return err
}

// MelodyStatus is where a song's melody stands: the recording it comes
// from (as GetSongMelody picks it), its split, the notes found in it, and
// what's needed to say how long it'll be.
type MelodyStatus struct {
	AudioID  int64
	Category AudioCategory
	ByteSize int64
	// Split: its vocals part is there; Found: its notes were looked for
	// (OK: there's a melody).
	Split, Found, OK bool
	// Job: its stems job's status ("" none, "queued", "running", "done",
	// "failed"), its attempts and last error.
	Job      string
	Attempts int
	Error    string
	// Started: when the running job started; Progress, the worker's
	// report for this run (nil: none).
	Started  time.Time
	Progress *float64
	// Ahead: the splits a worker takes before this queued one (those
	// running, and those queued earlier), AheadBytes their recordings'
	// sizes, AheadRunning those running (their own Started and size).
	Ahead        int
	AheadBytes   int64
	AheadRunning []RunningSplit
	// Analysis: the recording's analysis job ("" none, "queued",
	// "running", "done", "failed"; "done" when there's an analysis).
	Analysis string
	// WorkerSeen: when a stems worker was last at work (zero: never).
	WorkerSeen time.Time
	// SecondsPerMB: how long the worker's recent splits took per MB of
	// recording (0: none to go by); NotesSeconds: how long finding a
	// melody's notes took lately.
	SecondsPerMB float64
	NotesSeconds float64
	// NotesAhead: the vocals parts whose notes are found before this
	// one's (the newest first).
	NotesAhead int
	Now        time.Time
}

// RunningSplit is a split being made: since when, of how big a
// recording, and how far it is (nil: not reported).
type RunningSplit struct {
	Started  time.Time
	ByteSize int64
	Progress *float64
}

// GetMelodyStatus is where a song's melody stands; nil when it has no
// recording a melody can come from.
func (st *Store) GetMelodyStatus(ctx context.Context, variantID int64, analyzer string) (*MelodyStatus, error) {
	var m MelodyStatus
	var started, queued, stemAt *time.Time
	var seen *time.Time
	err := st.db.QueryRow(ctx, `
		SELECT a.id, a.category, a.byte_size, s.song_audio_id IS NOT NULL, mn.song_audio_id IS NOT NULL, coalesce(mn.ok, false),
		       coalesce(j.status, ''), coalesce(j.attempts, 0), coalesce(j.error, ''), j.started_at, j.queued_at,
		       CASE WHEN j.status = 'running' AND j.progress_at >= j.started_at THEN j.progress END,
		       CASE WHEN x.song_audio_id IS NOT NULL THEN 'done' ELSE coalesce(aj.status, '') END,
		       s.created_at,
		       (SELECT max(seen_at) FROM stem_workers), now()
		FROM song_audio a
		LEFT JOIN song_audio_stems s ON s.song_audio_id = a.id AND s.stem = 'vocals' AND s.source_sha256 = a.sha256
		LEFT JOIN song_melody_notes mn ON mn.song_audio_id = a.id AND mn.stem_created_at = s.created_at AND mn.stem_sha256 = s.source_sha256 AND mn.analyzer = $2
		LEFT JOIN song_audio_stem_jobs j ON j.song_audio_id = a.id AND j.source_sha256 = a.sha256
		LEFT JOIN song_audio_analysis_jobs aj ON aj.song_audio_id = a.id AND aj.source_sha256 = a.sha256
		LEFT JOIN song_audio_analysis x ON x.song_audio_id = a.id AND x.source_sha256 = a.sha256
		WHERE a.variant_id = $1 AND a.category IN ('youtube', 'band')
		ORDER BY coalesce(mn.ok, false) DESC, s.song_audio_id IS NOT NULL DESC, CASE a.category WHEN 'youtube' THEN 0 ELSE 1 END
		LIMIT 1`, variantID, analyzer).Scan(&m.AudioID, &m.Category, &m.ByteSize, &m.Split, &m.Found, &m.OK,
		&m.Job, &m.Attempts, &m.Error, &started, &queued, &m.Progress, &m.Analysis, &stemAt, &seen, &m.Now)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if started != nil {
		m.Started = *started
	}
	if seen != nil {
		m.WorkerSeen = *seen
	}
	// How fast the worker has been: its last splits' time per MB of
	// recording (the split alone: a job is done when its parts are saved,
	// before the analysis).
	if err := st.db.QueryRow(ctx, `
		SELECT coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY t), 0) FROM (
			SELECT extract(epoch FROM j.finished_at - j.started_at) / (a.byte_size / 1e6) AS t
			FROM song_audio_stem_jobs j JOIN song_audio a ON a.id = j.song_audio_id
			WHERE j.status = 'done' AND j.finished_at > j.started_at AND a.byte_size > 100000
			ORDER BY j.finished_at DESC LIMIT 20) r`).Scan(&m.SecondsPerMB); err != nil {
		return nil, err
	}
	switch {
	case m.Job == "queued" && queued != nil:
		// As the worker takes them (store.ClaimStemJob): the oldest first,
		// never a Guitar Pro backing track.
		if err := st.db.QueryRow(ctx, `
			SELECT count(*), coalesce(sum(a.byte_size), 0)::bigint FROM song_audio_stem_jobs j
			JOIN song_audio a ON a.id = j.song_audio_id AND a.category <> 'gp'
			WHERE j.status = 'queued' AND j.queued_at < $1 AND j.song_audio_id <> $2`, *queued, m.AudioID).Scan(&m.Ahead, &m.AheadBytes); err != nil {
			return nil, err
		}
		rows, err := st.db.Query(ctx, `
			SELECT j.started_at, a.byte_size, CASE WHEN j.progress_at >= j.started_at THEN j.progress END
			FROM song_audio_stem_jobs j JOIN song_audio a ON a.id = j.song_audio_id
			WHERE j.status = 'running' AND j.song_audio_id <> $1
			  AND j.started_at > now() - interval '40 minutes'`, m.AudioID) // a run older is a worker gone
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var r RunningSplit
			if err := rows.Scan(&r.Started, &r.ByteSize, &r.Progress); err != nil {
				return nil, err
			}
			m.AheadRunning = append(m.AheadRunning, r)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		m.Ahead += len(m.AheadRunning)
	case m.Split && !m.Found && stemAt != nil:
		if err := st.db.QueryRow(ctx, `
			SELECT coalesce((SELECT avg(took_seconds) FROM (SELECT took_seconds FROM song_melody_notes ORDER BY created_at DESC LIMIT 20) t), 0),
			       (SELECT count(*) FROM song_audio_stems s
			        JOIN song_audio a ON a.id = s.song_audio_id AND a.sha256 = s.source_sha256 AND a.category <> 'gp'
			        LEFT JOIN song_melody_notes mn ON mn.song_audio_id = s.song_audio_id
			        WHERE s.stem = 'vocals' AND s.created_at > $1
			          AND (mn.song_audio_id IS NULL OR mn.stem_created_at <> s.created_at OR mn.analyzer <> $2))`,
			*stemAt, analyzer).Scan(&m.NotesSeconds, &m.NotesAhead); err != nil {
			return nil, err
		}
	}
	return &m, nil
}
