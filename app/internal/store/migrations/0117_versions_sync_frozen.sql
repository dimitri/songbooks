-- The ownership model, steps 6 and 7 (docs/ownership-model.md §5, §8).
--
-- The original's changes offered to its versions: a version records the
-- original's chart as it was when the version was made or last synced
-- with it (base_body, and the original's version number then,
-- base_version). When the original's chart changes, the version's owner
-- is told and sees what changed since base_body; taking the change, or
-- keeping theirs, syncs the version: base_body becomes the original's
-- chart of that moment. NULL: not a version (or a frozen copy, which
-- follows nothing).
--
-- The versions made before this migration count as synced now: what the
-- original did before is never offered (nobody was told of it).
ALTER TABLE songs
  ADD COLUMN base_body text,
  ADD COLUMN base_version integer,
  ADD COLUMN base_synced_at timestamptz;
UPDATE songs v SET base_body = o.body, base_version = o.version, base_synced_at = now()
FROM songs o WHERE v.forked_from_id = o.id;

-- Frozen copies: making a song private, or deleting it, while songbooks
-- of other owners link to it gives each of those owners a copy as it
-- was, private, theirs, its forked_from_id the song (NULL once the song
-- is deleted: hence ON DELETE SET NULL) and marked: frozen_from is the
-- song's title and artist as they were ("Gin Lane — Mercy").
ALTER TABLE songs
  ADD COLUMN frozen_from text NOT NULL DEFAULT '',
  ADD COLUMN frozen_at timestamptz;
ALTER TABLE songs DROP CONSTRAINT songs_forked_from_id_fkey;
ALTER TABLE songs ADD CONSTRAINT songs_forked_from_id_fkey
  FOREIGN KEY (forked_from_id) REFERENCES songs(id) ON DELETE SET NULL;

-- Band recordings, public (§8): a band's recordings of its own public
-- song are public with it (§2); a band's core member may also make one
-- recording of a public song the band only links public, one by one.
ALTER TABLE band_recordings ADD COLUMN public boolean NOT NULL DEFAULT false;
