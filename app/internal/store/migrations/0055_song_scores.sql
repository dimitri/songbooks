-- song_scores: the LilyPond-subset source text a .song section's
-- "score:" field points at (internal/song.Section.Score — a filename,
-- resolved here as one row per (variant, section_index) rather than an
-- actual filesystem path; there is no scores/ directory on disk in this
-- deployment, Postgres is the only store). Parsed by internal/tab's
-- ParseScore and spliced into that section's chord grid by
-- Store.LoadVariant — see that function's own comment.
CREATE TABLE song_scores (
  id                   bigserial PRIMARY KEY,
  variant_id           bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  section_index        int NOT NULL,
  ly_source            text NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  created_by_person_id bigint REFERENCES people(id)
);
-- One score per section per variant — re-saving the same section's score
-- overwrites it, matching song_audio's one-row-per-category shape.
CREATE UNIQUE INDEX song_scores_variant_section_idx ON song_scores(variant_id, section_index);
