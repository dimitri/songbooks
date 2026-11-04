-- song_audio: one reference-recording MP3 per song variant, stored as
-- bytea directly in Postgres rather than a filesystem volume or object
-- storage — see app/docs/mp3-reference-audio.md for the full reasoning
-- (storage decision, the yt-dlp download convenience, and what to revisit
-- if this deployment ever grows past invite-only/friends-only use).
CREATE TABLE song_audio (
  id                   bigserial PRIMARY KEY,
  variant_id           bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  content_type         text NOT NULL DEFAULT 'audio/mpeg',
  byte_size            bigint NOT NULL,
  sha256               text NOT NULL,
  source_url           text NOT NULL DEFAULT '',
  data                 bytea NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by_person_id bigint REFERENCES people(id)
);
-- One reference track per song variant for v1 — a "pick a different
-- take" flow is a real future feature, not needed to ship this.
CREATE UNIQUE INDEX song_audio_variant_idx ON song_audio(variant_id);

-- song_audio_markers: a timestamp tied to one of the song's own sections
-- (by position in the parsed Section list, not a separate id of its own —
-- there's no persistent per-section id in the .song grammar itself).
CREATE TABLE song_audio_markers (
  id            bigserial PRIMARY KEY,
  song_audio_id bigint NOT NULL REFERENCES song_audio(id) ON DELETE CASCADE,
  section_index int NOT NULL,
  time_seconds  numeric NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX song_audio_markers_unique ON song_audio_markers(song_audio_id, section_index);
