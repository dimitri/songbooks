-- What a song's Guitar Pro file says about the song (tempo, time and key
-- signatures, markers, tracks, chords, chord diagrams, played bar order),
-- read from the file by static/js/gp-facts.js and stored next to it, per
-- file version. The song's text is compared with it to flag drift
-- (docs/gp-file-model.md §4). NULL until a browser or tools/gp has read
-- the file.
ALTER TABLE song_gp_source_file ADD COLUMN facts jsonb;
ALTER TABLE song_gp_source_file_version ADD COLUMN facts jsonb;

-- A difference between the text and the file someone chose to keep
-- ("Keep both"). sig hashes the two values it was taken on, so the
-- difference comes back as soon as either side changes.
CREATE TABLE gp_drift_ack (
  variant_id bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  fact       text NOT NULL,
  sig        text NOT NULL,
  person_id  bigint REFERENCES people(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (variant_id, fact)
);
