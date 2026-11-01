-- School-owned variants: shared material that belongs to a school rather
-- than to one person or band. Added as a new value on the existing
-- variant_visibility enum (same pattern migration 0011 used for 'band'
-- and migration 0014 for 'jam') plus a new owner_school_id column
-- parallel to owner_band_id. Left in its own migration file, separate
-- from migration 0023 which already created the schools table: Postgres
-- won't let a new enum value be used by a DML statement in the same
-- transaction that adds it, and each migration file runs in its own
-- transaction (see internal/store/migrate.go) — the same lesson already
-- learned in migrations 0011/0014.
ALTER TYPE variant_visibility ADD VALUE 'school';

ALTER TABLE songs
  ADD COLUMN owner_school_id bigint REFERENCES schools(id);
CREATE INDEX songs_owner_school_idx ON songs (owner_school_id) WHERE owner_school_id IS NOT NULL;
