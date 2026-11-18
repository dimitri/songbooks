-- A song's reference recordings split into their parts (Demucs: vocals,
-- drums, bass, other, and no_vocals, the three others mixed), for the
-- practice player's voice control and the Mixer's lanes. Made in the
-- background by cmd/stemsd, from song_audio_stem_jobs; see
-- app/docs/audio-cast-transpose-stems.md.
CREATE TABLE song_audio_stems (
  song_audio_id bigint NOT NULL REFERENCES song_audio(id) ON DELETE CASCADE,
  stem text NOT NULL CHECK (stem IN ('vocals', 'drums', 'bass', 'other', 'no_vocals')),
  -- The recording it was split from: a replaced recording's old stems
  -- are never served for the new one.
  source_sha256 text NOT NULL,
  content_type text NOT NULL,
  byte_size bigint NOT NULL,
  data bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (song_audio_id, stem)
);

-- One job per recording: queued when it's added or replaced (the trigger
-- below, whatever wrote it: an upload, a YouTube download, a Guitar Pro
-- import, songimport), taken by the worker.
CREATE TABLE song_audio_stem_jobs (
  song_audio_id bigint PRIMARY KEY REFERENCES song_audio(id) ON DELETE CASCADE,
  source_sha256 text NOT NULL,
  status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
  model text NOT NULL DEFAULT '',
  attempts int NOT NULL DEFAULT 0,
  error text NOT NULL DEFAULT '',
  queued_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz,
  finished_at timestamptz
);
CREATE INDEX song_audio_stem_jobs_queued_idx ON song_audio_stem_jobs (queued_at) WHERE status = 'queued';

CREATE FUNCTION song_audio_queue_stems() RETURNS trigger AS $$
BEGIN
  INSERT INTO song_audio_stem_jobs (song_audio_id, source_sha256)
  VALUES (NEW.id, NEW.sha256)
  ON CONFLICT (song_audio_id) DO UPDATE SET
    source_sha256 = EXCLUDED.source_sha256, status = 'queued', attempts = 0,
    error = '', queued_at = now(), started_at = NULL, finished_at = NULL;
  PERFORM pg_notify('song_audio_stems', NEW.id::text);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER song_audio_queue_stems_insert AFTER INSERT ON song_audio
  FOR EACH ROW EXECUTE FUNCTION song_audio_queue_stems();
CREATE TRIGGER song_audio_queue_stems_update AFTER UPDATE OF sha256 ON song_audio
  FOR EACH ROW WHEN (OLD.sha256 IS DISTINCT FROM NEW.sha256) EXECUTE FUNCTION song_audio_queue_stems();

-- The recordings already there.
INSERT INTO song_audio_stem_jobs (song_audio_id, source_sha256)
SELECT id, sha256 FROM song_audio;
