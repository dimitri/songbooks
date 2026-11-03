-- music_type: whether a band plays other people's songs or its own.
-- Nullable/unset is the honest default for an existing band until
-- someone says which it is; no enum type, just a checked text column
-- (same "free text, no fixed vocabulary" philosophy as works.genre,
-- migration 0030), since a future "both" or other nuance shouldn't need
-- a migration to express.
ALTER TABLE bands ADD COLUMN music_type text
  CHECK (music_type IS NULL OR music_type IN ('covers', 'original'));

-- album: a property of the WORK (the abstract song), not one variant —
-- same reasoning as works.genre (migration 0030). Matters mainly for an
-- "original" band, whose repertoire reads naturally grouped by the
-- record it came from; a covers band has no such concept and this stays
-- unset for it.
ALTER TABLE works ADD COLUMN album text;
CREATE INDEX works_album_idx ON works (album) WHERE album IS NOT NULL;
