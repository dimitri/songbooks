-- The YouTube reference's audio, fetched in the background
-- (docs/mp3-reference-audio.md "Automatic"; store/youtube_audio_jobs.go,
-- httpapi/yt_audio_jobs.go): the song's MP3 that the click, the sync, the
-- parts and the melody all start from, fetched by yt-dlp in the app's own
-- small worker pool instead of inside an editor's request.
--
-- One job per song, for one link (url): a new link replaces it. States:
--   queued   waiting (run_after: not before then, the backoff after a
--            failure that may pass: rate-limited, the network)
--   running  a worker has it: progress 0..1 and eta_seconds from yt-dlp's
--            progress lines, heartbeat_at its last sign of life (a job
--            whose worker went quiet is queued again)
--   done     the audio is stored (song_audio, category 'youtube'), which
--            queues its split, then its analysis (migrations 0078, 0106)
--   failed   for good: reason says why (age_restricted, unavailable,
--            blocked, too_long, other), error yt-dlp's last words
--   removed  an editor removed the audio: not fetched again for this link
--            until someone asks
-- trigger: what queued it (reference, open, retry, backfill, wizard).
CREATE TABLE song_youtube_audio_jobs (
  variant_id      bigint PRIMARY KEY REFERENCES songs(id) ON DELETE CASCADE,
  url             text NOT NULL,
  status          text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed', 'removed')),
  reason          text NOT NULL DEFAULT '' CHECK (reason IN ('', 'age_restricted', 'unavailable', 'blocked', 'too_long', 'other')),
  error           text NOT NULL DEFAULT '',
  attempts        int NOT NULL DEFAULT 0,
  progress        real,
  eta_seconds     int,
  duration_s      double precision,
  trigger         text NOT NULL DEFAULT '',
  requested_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  queued_at       timestamptz NOT NULL DEFAULT now(),
  run_after       timestamptz NOT NULL DEFAULT now(),
  started_at      timestamptz,
  heartbeat_at    timestamptz,
  finished_at     timestamptz
);
CREATE INDEX song_youtube_audio_jobs_queued_idx ON song_youtube_audio_jobs (run_after) WHERE status = 'queued';

-- A link whose audio yt-dlp can fetch: a YouTube video (watch, youtu.be,
-- shorts, live, embed), not a channel or a playlist. store.YouTubeID's
-- shapes.
CREATE FUNCTION is_youtube_video_url(u text) RETURNS boolean AS $$
  SELECT coalesce(btrim(u), '') ~* '^https?://((www|m|music)\.)?(youtube\.com/(watch\?|shorts/|live/|embed/)|youtu\.be/[A-Za-z0-9_-]|youtube-nocookie\.com/embed/)'
$$ LANGUAGE sql IMMUTABLE;

-- A song's reference set or changed, however it was (the editor, the
-- YouTube picker, quick add, The Big Songbook's fixes and new
-- recordings, proposals, imports): its audio is queued, unless it's
-- there already for that link or a job for it is under way. A link
-- taken away (or one that isn't a video), or put back to one whose audio
-- is there: the job for another link goes.
CREATE FUNCTION song_queue_youtube_audio() RETURNS trigger AS $$
DECLARE
  ref text := btrim(coalesce(NEW.reference_url, ''));
BEGIN
  IF TG_OP = 'UPDATE' AND btrim(coalesce(OLD.reference_url, '')) = ref THEN
    RETURN NEW;
  END IF;
  IF NOT is_youtube_video_url(ref) THEN
    DELETE FROM song_youtube_audio_jobs WHERE variant_id = NEW.id;
    RETURN NEW;
  END IF;
  -- Its audio there already (a link put back): nothing to fetch, and
  -- another link's job goes.
  IF EXISTS (SELECT 1 FROM song_audio WHERE variant_id = NEW.id AND category = 'youtube' AND btrim(source_url) = ref) THEN
    DELETE FROM song_youtube_audio_jobs WHERE variant_id = NEW.id AND url <> ref;
    RETURN NEW;
  END IF;
  INSERT INTO song_youtube_audio_jobs (variant_id, url, trigger)
  VALUES (NEW.id, ref, 'reference')
  ON CONFLICT (variant_id) DO UPDATE SET
    url = EXCLUDED.url, status = 'queued', reason = '', error = '', attempts = 0,
    progress = NULL, eta_seconds = NULL, duration_s = NULL, trigger = EXCLUDED.trigger,
    requested_by_person_id = NULL, queued_at = now(), run_after = now(),
    started_at = NULL, heartbeat_at = NULL, finished_at = NULL
  WHERE NOT (song_youtube_audio_jobs.url = EXCLUDED.url AND song_youtube_audio_jobs.status IN ('queued', 'running'));
  PERFORM pg_notify('song_youtube_audio', NEW.id::text);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER song_queue_youtube_audio_insert AFTER INSERT ON songs
  FOR EACH ROW WHEN (is_youtube_video_url(NEW.reference_url)) EXECUTE FUNCTION song_queue_youtube_audio();
CREATE TRIGGER song_queue_youtube_audio_update AFTER UPDATE OF reference_url ON songs
  FOR EACH ROW EXECUTE FUNCTION song_queue_youtube_audio();

-- The songs there already are queued by cmd/backfillaudio, slowly, when
-- someone runs it; and each as it's opened (store.EnsureYouTubeAudio).
