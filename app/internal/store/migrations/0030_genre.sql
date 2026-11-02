-- Genre: a property of the SONG (the abstract work — "Mercy, Mercy,
-- Mercy" is a soul tune regardless of whose chord chart you're looking
-- at), not of one person's/band's particular variant/arrangement of it —
-- so it belongs on works, the same reasoning migration 0008's own
-- comment gives for title/artist living there rather than being
-- duplicated per-variant. Free text, no fixed enum, same philosophy as
-- people.instruments (migration 0018): "Rock"/"rock"/"classic rock" are
-- all valid, nothing here normalizes them — the catalog filter groups by
-- the literal distinct values present, not a controlled vocabulary.
ALTER TABLE works ADD COLUMN genre text;
CREATE INDEX works_genre_idx ON works (genre) WHERE genre IS NOT NULL;
