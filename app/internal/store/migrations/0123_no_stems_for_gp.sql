-- A Guitar Pro file's backing track (a song's "gp" recording: a guitar
-- lesson's, mostly) isn't split into parts: it's a backing track already,
-- the lesson's instrument left out or played along, and its parts were
-- never what its player offers (no voice, no karaoke). A new or replaced
-- "gp" recording is no longer queued; the worker skips any still queued
-- (store.ClaimStemJob), and the parts already made are kept but no longer
-- served (store.GetStemStatus, GetStemData).
CREATE OR REPLACE FUNCTION song_audio_queue_stems() RETURNS trigger AS $$
BEGIN
  IF NEW.category = 'gp' THEN
    RETURN NEW;
  END IF;
  INSERT INTO song_audio_stem_jobs (song_audio_id, source_sha256)
  VALUES (NEW.id, NEW.sha256)
  ON CONFLICT (song_audio_id) DO UPDATE SET
    source_sha256 = EXCLUDED.source_sha256, status = 'queued', attempts = 0,
    error = '', queued_at = now(), started_at = NULL, finished_at = NULL;
  PERFORM pg_notify('song_audio_stems', NEW.id::text);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
