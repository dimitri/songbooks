-- Phase 1 of the data-model rewrite ("Work vs variant"). Introduces
-- `works` — a lightweight title+artist identity used for grouping "all
-- versions of this song" across the catalog — and turns `songs` into
-- variants: one specific arrangement (body/grid/lyrics) of a work.
--
-- The table keeps the name `songs` (it's referenced by FK from
-- setlists/jam_* in ways that aren't worth touching this phase): from here
-- on, read `songs` as "variants" conceptually. See also
-- internal/store/catalog.go.
--
-- visibility is 'global' (catalog-wide content, owner_person_id NULL —
-- every existing imported song becomes this) or 'user' (a person's own
-- fork/copy, owner_person_id set). Later phases add 'band'/'jam' values to
-- this same enum rather than a new column.
CREATE TYPE variant_visibility AS ENUM ('global', 'user');

CREATE TABLE works (
  id          bigserial PRIMARY KEY,
  title       text NOT NULL,
  artist      text NOT NULL DEFAULT '',
  -- Simple normalization (lowercase, trim) for "is this the same work"
  -- matching in FindOrCreateWork — not fuzzy matching, just enough to
  -- dedupe exact-same title/artist pairs typed with different casing or
  -- stray whitespace.
  norm_title  text GENERATED ALWAYS AS (lower(trim(title))) STORED,
  norm_artist text GENERATED ALWAYS AS (lower(trim(artist))) STORED,
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (norm_title, norm_artist)
);
CREATE INDEX works_artist_idx ON works (norm_artist);

ALTER TABLE songs
  ADD COLUMN work_id             bigint REFERENCES works(id),
  ADD COLUMN visibility          variant_visibility NOT NULL DEFAULT 'global',
  ADD COLUMN owner_person_id     bigint REFERENCES people(id),
  ADD COLUMN forked_from_id      bigint REFERENCES songs(id),
  ADD COLUMN created_by_person_id bigint REFERENCES people(id);

-- Backfill: every existing row is visibility='global' (the column
-- default) already, so this just needs a work_id. Match-or-create a work
-- per distinct normalized (title, artist) pair already in the table.
INSERT INTO works (title, artist)
SELECT DISTINCT title, artist FROM songs
ON CONFLICT (norm_title, norm_artist) DO NOTHING;

UPDATE songs s
SET work_id = w.id
FROM works w
WHERE w.norm_title = lower(trim(s.title)) AND w.norm_artist = lower(trim(s.artist))
  AND s.work_id IS NULL;

ALTER TABLE songs ALTER COLUMN work_id SET NOT NULL;
CREATE INDEX songs_work_idx ON songs (work_id);
CREATE INDEX songs_owner_idx ON songs (owner_person_id) WHERE owner_person_id IS NOT NULL;
