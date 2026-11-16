-- Downloads are a privilege of a community role (docs/community-downloads.md):
-- download_files to download (and play, and open) a songbook's files,
-- manage_files to add, move and remove them. Every role but guest could
-- download before, staff could manage: keep it so.
INSERT INTO community_role_privileges (role_id, privilege)
  SELECT id, 'download_files' FROM community_roles WHERE builtin_key IS DISTINCT FROM 'guest'
  UNION ALL
  SELECT id, 'manage_files' FROM community_roles WHERE builtin_key = 'staff'
ON CONFLICT DO NOTHING;

-- Who downloaded what, for a community's stats page. One row per download
-- (a range request that doesn't start the file isn't one). name and
-- byte_size are kept as they were, for a file since removed; a zip names
-- the folder it was made of.
CREATE TABLE songbook_downloads (
  id           bigserial PRIMARY KEY,
  community_id bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  songbook_id  bigint REFERENCES songbooks(id) ON DELETE CASCADE,
  file_id      bigint REFERENCES songbook_files(id) ON DELETE SET NULL,
  person_id    bigint REFERENCES people(id) ON DELETE SET NULL,
  what         text NOT NULL CHECK (what IN ('download', 'open', 'play', 'zip')),
  name         text NOT NULL,
  byte_size    bigint NOT NULL DEFAULT 0,
  at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX songbook_downloads_community_idx ON songbook_downloads (community_id, at);
CREATE INDEX songbook_downloads_file_idx ON songbook_downloads (file_id);

-- A song page's views, per person: how many, and since when (the visit
-- row already says when last).
ALTER TABLE song_visits
  ADD COLUMN views int NOT NULL DEFAULT 1,
  ADD COLUMN first_visited_at timestamptz;
UPDATE song_visits SET first_visited_at = visited_at;
ALTER TABLE song_visits
  ALTER COLUMN first_visited_at SET DEFAULT now(),
  ALTER COLUMN first_visited_at SET NOT NULL;
