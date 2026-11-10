-- The ORIGINAL .gp/.gp3/.gp4/.gp5/.gpx file a GP import was run from,
-- kept so community members can download the real Guitar Pro file
-- alongside the rendered chart/tab — today the import only persists
-- what it extracted (the backing track in song_audio, the riff JSON in
-- song_scores), never the source file itself. One per variant (a later
-- import overwrites the one already here, same "latest wins" posture
-- song_audio's own per-category row already has) — stored as bytea in
-- Postgres, same reasoning as song_audio (see its own migration/doc
-- comment): no filesystem volume or object storage to provision/back up
-- separately.
CREATE TABLE song_gp_source_file (
  variant_id bigint PRIMARY KEY REFERENCES songs(id) ON DELETE CASCADE,
  filename text NOT NULL,
  content_type text NOT NULL,
  byte_size bigint NOT NULL,
  sha256 text NOT NULL,
  data bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by_person_id bigint REFERENCES people(id)
);
