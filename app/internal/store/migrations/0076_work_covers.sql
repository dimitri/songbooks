-- A song's cover art (docs/covers.md), found by its title and artist
-- (iTunes, then Deezer, then its YouTube reference) and kept per work, so
-- every version of the song shares it: the image's address, the album's
-- page and the image's colors, never the image itself. found false: looked
-- for and not found, not looked for again unless asked.
CREATE TABLE work_covers (
  work_id   bigint PRIMARY KEY REFERENCES works(id) ON DELETE CASCADE,
  found     boolean NOT NULL,
  cover     jsonb NOT NULL DEFAULT '{}',
  looked_at timestamptz NOT NULL DEFAULT now()
);
