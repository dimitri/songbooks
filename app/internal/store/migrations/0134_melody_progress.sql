-- Where a song's melody stands, for its Melody view while it can't be
-- shown yet (docs/melody-sheet.md, "Until it's ready"; GET
-- /api/songs/{id}/melody/status): the voice separation queued, running,
-- failed, or not done on this server at all.
--
-- progress: the share of Demucs's separation done (0..1), reported by
-- the worker as Demucs prints it (cmd/stemsd), every few per cent;
-- progress_at when. Meaningful only while the job runs and progress_at
-- is after started_at (a job queued again keeps its last run's).
ALTER TABLE song_audio_stem_jobs ADD COLUMN progress real, ADD COLUMN progress_at timestamptz;

-- The stems workers seen at work: each notes itself here when it looks
-- for a job (at least once a minute while idle) and as it reports a
-- job's progress. None seen lately: this server splits nothing (staging
-- has no worker), and the Melody view says so rather than "queued".
CREATE TABLE stem_workers (
  name    text PRIMARY KEY,
  seen_at timestamptz NOT NULL DEFAULT now()
);
