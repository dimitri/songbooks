-- Band-owned variants: shared, collaboratively-edited material that
-- belongs to a band rather than to one person. Added as a new value on
-- the existing variant_visibility enum (per migration 0008's own comment:
-- "Later phases add 'band'/'jam' values to this same enum rather than a
-- new column") plus a new owner_band_id column parallel to
-- owner_person_id. Left in its own migration file, separate from anything
-- that uses the literal 'band' value, because Postgres won't let a new
-- enum value be used by a DML statement in the same transaction that adds
-- it.
ALTER TYPE variant_visibility ADD VALUE 'band';

ALTER TABLE songs
  ADD COLUMN owner_band_id bigint REFERENCES bands(id);
CREATE INDEX songs_owner_band_idx ON songs (owner_band_id) WHERE owner_band_id IS NOT NULL;
