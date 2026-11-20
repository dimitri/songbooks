-- What a reference recording itself says: its beats and bars, metre,
-- tempo, key and chords, and where the voice is. Found once per
-- recording by the stems worker right after the split (cmd/stemsd,
-- deploy/analysis/sbanalyze.py: Beat This! and lv-chordia), independent
-- of the chart; the chart is aligned to it at request time
-- (internal/chartalign). See app/reports/auto-sync-chords.md.
CREATE TABLE song_audio_analysis (
  song_audio_id bigint PRIMARY KEY REFERENCES song_audio(id) ON DELETE CASCADE,
  -- The recording it was made from: a replaced recording's analysis is
  -- never used for the new one.
  source_sha256 text NOT NULL,
  analyzer text NOT NULL,           -- 'beat_this:1.1.0/final0+lv_chordia:1.1.0+sbanalyze:1'
  created_at timestamptz NOT NULL DEFAULT now(),
  duration_s double precision NOT NULL,
  tempo_bpm double precision,       -- median beat
  tempo_cv real,                    -- steadiness: < 0.03 steady, > 0.08 rubato or live
  time_sig text,                    -- '4/4', '3/4', '6/8', '12/8'
  time_sig_conf real,               -- share of the bars with that many beats
  key text,
  beat_conf real,                   -- the beat tracker's mean activation at its beats
  -- Everything else, a few tens of KB (chartalign.Recording): beats and
  -- their place in the bar, chord segments, the likeliest chords per
  -- beat, the voice's phrases.
  data jsonb NOT NULL
);

-- One job per recording, the same shape as song_audio_stem_jobs. Queued
-- when its split is saved (the trigger below), so analysing never delays
-- a split; the worker usually does it at once on the WAVs it has, and a
-- job left queued (a crash, a recording split before this migration, a
-- new model) is done later from the stored parts.
CREATE TABLE song_audio_analysis_jobs (
  song_audio_id bigint PRIMARY KEY REFERENCES song_audio(id) ON DELETE CASCADE,
  source_sha256 text NOT NULL,
  status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
  analyzer text NOT NULL DEFAULT '',
  attempts int NOT NULL DEFAULT 0,
  error text NOT NULL DEFAULT '',
  queued_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz,
  finished_at timestamptz
);
CREATE INDEX song_audio_analysis_jobs_queued_idx ON song_audio_analysis_jobs (queued_at) WHERE status = 'queued';

CREATE FUNCTION song_audio_queue_analysis() RETURNS trigger AS $$
BEGIN
  INSERT INTO song_audio_analysis_jobs (song_audio_id, source_sha256)
  VALUES (NEW.song_audio_id, NEW.source_sha256)
  ON CONFLICT (song_audio_id) DO UPDATE SET
    source_sha256 = EXCLUDED.source_sha256, status = 'queued', attempts = 0,
    error = '', queued_at = now(), started_at = NULL, finished_at = NULL;
  PERFORM pg_notify('song_audio_stems', NEW.song_audio_id::text);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER song_audio_queue_analysis AFTER UPDATE OF status ON song_audio_stem_jobs
  FOR EACH ROW WHEN (NEW.status = 'done' AND OLD.status IS DISTINCT FROM 'done') EXECUTE FUNCTION song_audio_queue_analysis();

-- The recordings already split.
INSERT INTO song_audio_analysis_jobs (song_audio_id, source_sha256)
SELECT j.song_audio_id, j.source_sha256
FROM song_audio_stem_jobs j JOIN song_audio a ON a.id = j.song_audio_id AND a.sha256 = j.source_sha256
WHERE j.status = 'done';
