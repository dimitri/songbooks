-- The ownership model, step 1: the data model (docs/ownership-model.md).
-- A song has one owner — a person, a band or a community — and is
-- private (the owner's members) or public (everyone signed in). The
-- catalogue's songs, ownerless today, are owned by The Big Songbook
-- (« Le Grand Songbook »), the global community: everybody belongs to it
-- implicitly (no community_members rows), its staff are the site admins.
--
-- This migration only adds the columns and The Big Songbook's row. Who
-- owns what, and what is public, is cmd/ownership's (a dry run first,
-- then --apply); variant_visibility stays as it is, and the app keeps
-- reading it, until step 2 switches the code to these columns and drops
-- it.

-- The global community: one row, kind 'global'.
ALTER TABLE communities DROP CONSTRAINT communities_kind_check;
ALTER TABLE communities ADD CONSTRAINT communities_kind_check
  CHECK (kind IN ('community', 'coaching', 'global'));
CREATE UNIQUE INDEX communities_one_global_idx ON communities ((kind)) WHERE kind = 'global';

-- big_songbook_id() is The Big Songbook's id, its row made when missing
-- (a fresh database, a test that emptied communities): the songs it owns
-- are found through it, never through a fixed id.
CREATE FUNCTION big_songbook_id() RETURNS bigint LANGUAGE plpgsql AS $$
DECLARE
  cid bigint;
BEGIN
  SELECT id INTO cid FROM communities WHERE kind = 'global';
  IF cid IS NULL THEN
    INSERT INTO communities (name, kind) VALUES ('The Big Songbook', 'global')
    ON CONFLICT ((kind)) WHERE kind = 'global' DO NOTHING
    RETURNING id INTO cid;
    IF cid IS NULL THEN
      SELECT id INTO cid FROM communities WHERE kind = 'global';
    END IF;
  END IF;
  RETURN cid;
END $$;

SELECT big_songbook_id();

-- A song is public (everyone signed in sees it, links it, makes their own
-- version of it) or private (its owner's members). A version has a name
-- beside its title when its owner has more than one of the same song
-- ("Bad Spell — psychedelic"); "" for the one version most songs have.
-- owner_kind says which of owner_person_id / owner_band_id /
-- owner_community_id is the owner (NULL while none is: the catalogue's
-- songs until cmd/ownership gives them to The Big Songbook).
ALTER TABLE songs
  ADD COLUMN public boolean NOT NULL DEFAULT false,
  ADD COLUMN version_name text NOT NULL DEFAULT '',
  ADD COLUMN owner_kind text GENERATED ALWAYS AS (
    CASE WHEN owner_person_id IS NOT NULL THEN 'person'
         WHEN owner_band_id IS NOT NULL THEN 'band'
         WHEN owner_community_id IS NOT NULL THEN 'community'
    END) STORED;
CREATE INDEX songs_public_idx ON songs (work_id) WHERE public;

-- A songbook is public (everyone signed in opens it; its owner's songs in
-- it are public) or not; a closed songbook is a paid one (Florent's
-- methods, sold outside the app): never public, and its songs can't be
-- linked, copied or made into a version anywhere else. Opening it still
-- takes requires_grant's grant when it has one. `shared` (migration 0072)
-- is what closed replaces; cmd/ownership sets closed from it, and step 2
-- drops it.
ALTER TABLE songbooks
  ADD COLUMN public boolean NOT NULL DEFAULT false,
  ADD COLUMN closed boolean NOT NULL DEFAULT false,
  ADD CONSTRAINT songbooks_closed_not_public CHECK (NOT (public AND closed));
