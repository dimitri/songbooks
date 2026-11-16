-- A community's download area (docs/community-downloads.md): the files an
-- author hands out with a songbook — the book's PDF, the original Guitar
-- Pro files, the backing tracks — each belonging to one songbook, so the
-- songbook's own access decides who may download it. A file may go with
-- one song of the songbook (a lesson's own .gp and backing track).
--
-- The bytes live in 1 MiB chunks so a large PDF streams from the database
-- (and answers range requests) without being read whole into memory.
CREATE TABLE songbook_files (
  id                   bigserial PRIMARY KEY,
  songbook_id          bigint NOT NULL REFERENCES songbooks(id) ON DELETE CASCADE,
  variant_id           bigint REFERENCES songs(id) ON DELETE SET NULL,
  kind                 text NOT NULL CHECK (kind IN ('book', 'gp', 'audio', 'other')),
  filename             text NOT NULL,
  title                text NOT NULL DEFAULT '',
  content_type         text NOT NULL,
  byte_size            bigint NOT NULL,
  sha256               text NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  UNIQUE (songbook_id, sha256)
);
CREATE INDEX songbook_files_songbook_idx ON songbook_files (songbook_id);

CREATE TABLE songbook_file_chunks (
  file_id bigint NOT NULL REFERENCES songbook_files(id) ON DELETE CASCADE,
  n       int NOT NULL,
  data    bytea NOT NULL,
  PRIMARY KEY (file_id, n)
);

-- Each songbook is a product of its community: a grant is someone's right
-- to it, given by hand today and by a purchase once payments exist. The
-- grant says where it came from (a bundle buys each of its songbooks: one
-- grant per songbook, same reference), and may end.
ALTER TABLE songbook_access_grants
  ADD COLUMN source text NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'purchase', 'bundle')),
  ADD COLUMN reference text NOT NULL DEFAULT '',
  ADD COLUMN expires_at timestamptz;
