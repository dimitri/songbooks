-- The edit model, step 5 (docs/catalogue-edit-review.md §4.6): merge at
-- save. A song's revision says which version of the song its snapshot
-- is (the version the save started from): an editor saving over a
-- version the song has moved on from is merged with what changed since
-- (songdiff), the version it opened found here. NULL for the revisions
-- before (a save over one of those versions is refused as before: 409).
ALTER TABLE revisions ADD COLUMN song_version integer;
CREATE INDEX revisions_song_version_idx ON revisions (entity_id, song_version)
  WHERE entity_type = 'variant' AND song_version IS NOT NULL;
