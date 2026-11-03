-- project_albums: cover art + release year for one (project, album) pair,
-- keyed the same way works.album (migration 0033) already groups a
-- repertoire — no dedicated "albums" entity existed before this, since
-- album was only ever free text on a work. Storing the cover as an
-- external URL (not downloaded/rehosted) rather than inventing a new
-- upload/asset-storage layer: the app has no existing convention for
-- that, and these are the band's own official cover art from a stable
-- CDN, so hotlinking is the honest "don't over-build" choice here.
CREATE TABLE project_albums (
  project_id   bigint NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  album        text NOT NULL,
  cover_url    text NOT NULL,
  release_year int,
  credit       text NOT NULL DEFAULT '',
  PRIMARY KEY (project_id, album)
);
