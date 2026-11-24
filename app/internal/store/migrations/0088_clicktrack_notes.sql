-- A short note per song of a set list, shown large on the click track
-- while that song plays ("intro 4 bars piano alone"). Keyed by the set
-- list and the song (variant) in it: a song listed twice in the same set
-- list shares one note. Whoever may edit the set list may edit its notes.
CREATE TABLE setlist_clicktrack_notes (
  setlist_id bigint NOT NULL REFERENCES setlists(id) ON DELETE CASCADE,
  variant_id bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  note       text NOT NULL,
  updated_by bigint REFERENCES people(id) ON DELETE SET NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (setlist_id, variant_id)
);
