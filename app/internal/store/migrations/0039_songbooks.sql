-- Songbooks: the "owner" model rewrite's first piece. Every person and
-- every band gets exactly one or more named, ordered collections of
-- variant pointers ("my songbook", a band's repertoire renamed
-- "songbook") — replacing the single flat catalog_entries/
-- band_catalog_entries pointer tables with one shared, generalized
-- table that supports multiple named songbooks per owner (e.g. a band
-- with one songbook per album), per direct request. is_default marks
-- the one songbook every owner always has and that can be renamed but
-- never deleted down to zero.
CREATE TABLE songbooks (
  id               bigserial PRIMARY KEY,
  owner_kind       text NOT NULL CHECK (owner_kind IN ('person', 'band')),
  owner_person_id  bigint REFERENCES people(id) ON DELETE CASCADE,
  owner_band_id    bigint REFERENCES bands(id) ON DELETE CASCADE,
  name             text NOT NULL,
  is_default       boolean NOT NULL DEFAULT false,
  -- The songbook's own canonical play order (Set 1/Set 2/... groups of
  -- variant ids) — replaces projects.song_order, see migration 0041.
  -- []SongGroup{Label, VariantIDs} JSON-encoded; empty means "no explicit
  -- order yet, fall back to alphabetical" (same convention as before).
  song_order       jsonb NOT NULL DEFAULT '[]',
  playlist_url     text NOT NULL DEFAULT '', -- external (e.g. YouTube) playlist link, replaces projects.playlist_url
  created_at       timestamptz NOT NULL DEFAULT now(),
  CHECK (
    (owner_kind = 'person' AND owner_person_id IS NOT NULL AND owner_band_id IS NULL) OR
    (owner_kind = 'band'   AND owner_band_id IS NOT NULL   AND owner_person_id IS NULL)
  )
);
CREATE UNIQUE INDEX songbooks_default_person_idx ON songbooks (owner_person_id) WHERE is_default AND owner_kind = 'person';
CREATE UNIQUE INDEX songbooks_default_band_idx ON songbooks (owner_band_id) WHERE is_default AND owner_kind = 'band';
CREATE INDEX songbooks_owner_person_idx ON songbooks (owner_person_id) WHERE owner_person_id IS NOT NULL;
CREATE INDEX songbooks_owner_band_idx ON songbooks (owner_band_id) WHERE owner_band_id IS NOT NULL;

CREATE TABLE songbook_entries (
  id                  bigserial PRIMARY KEY,
  songbook_id         bigint NOT NULL REFERENCES songbooks(id) ON DELETE CASCADE,
  variant_id          bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  added_at            timestamptz NOT NULL DEFAULT now(),
  added_by_person_id  bigint REFERENCES people(id),
  UNIQUE (songbook_id, variant_id)
);
CREATE INDEX songbook_entries_songbook_idx ON songbook_entries (songbook_id);

-- One default songbook per existing person/band, so every owner already
-- has somewhere for FindOrCreateDefaultSongbook to land without a
-- first-use race.
INSERT INTO songbooks (owner_kind, owner_person_id, name, is_default)
SELECT 'person', id, 'My songbook', true FROM people;

INSERT INTO songbooks (owner_kind, owner_band_id, name, is_default)
SELECT 'band', id, 'Songbook', true FROM bands;

-- Fold the old flat pointer tables into the new default songbooks, then
-- retire them.
INSERT INTO songbook_entries (songbook_id, variant_id, added_at)
SELECT sb.id, ce.variant_id, ce.added_at
FROM catalog_entries ce
JOIN songbooks sb ON sb.owner_kind = 'person' AND sb.owner_person_id = ce.person_id AND sb.is_default;

INSERT INTO songbook_entries (songbook_id, variant_id, added_at, added_by_person_id)
SELECT sb.id, bce.variant_id, bce.added_at, bce.added_by_person_id
FROM band_catalog_entries bce
JOIN songbooks sb ON sb.owner_kind = 'band' AND sb.owner_band_id = bce.band_id AND sb.is_default;

DROP TABLE catalog_entries;
DROP TABLE band_catalog_entries;
