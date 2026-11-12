-- Earlier Guitar Pro files of a song. song_gp_source_file holds the current
-- one; uploading a different file (a re-import after editing it in Guitar
-- Pro) moves the one it replaces here instead of overwriting it, so every
-- version a band uploaded stays downloadable from the song's history.
CREATE TABLE song_gp_source_file_version (
  id                   bigserial PRIMARY KEY,
  variant_id           bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  filename             text NOT NULL,
  content_type         text NOT NULL,
  byte_size            bigint NOT NULL,
  sha256               text NOT NULL,
  data                 bytea NOT NULL,
  created_at           timestamptz NOT NULL,              -- when that file was uploaded
  replaced_at          timestamptz NOT NULL DEFAULT now(), -- when a newer one replaced it
  created_by_person_id bigint REFERENCES people(id)
);
CREATE INDEX song_gp_source_file_version_variant_idx ON song_gp_source_file_version(variant_id, replaced_at);
