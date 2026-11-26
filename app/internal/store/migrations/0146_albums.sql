-- Albums and live versions (docs/albums-and-live-versions.md; store/albums.go,
-- httpapi/albums.go).
--
-- An album (or EP, single, live album) is a band's release: its facts,
-- its uploaded artwork (album_covers, kept like a band's logo: cropped
-- square and resized by the server, never hotlinked), its links, and its
-- ordered tracks. A track is the band's song as printed on the release,
-- its master a file of the band's recordings folder (band_recordings),
-- streamed through the app, never copied. A track may have no song
-- (an interlude, a master the band never put in its songbook).
CREATE TABLE albums (
  id                   bigserial PRIMARY KEY,
  band_id              bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  title                text NOT NULL CHECK (btrim(title) <> ''),
  kind                 text NOT NULL DEFAULT 'album' CHECK (kind IN ('album', 'ep', 'single', 'live')),
  released_on          date,
  release_year         int CHECK (release_year IS NULL OR release_year BETWEEN 1900 AND 2200),
  label                text NOT NULL DEFAULT '',
  credits              text NOT NULL DEFAULT '',
  licence              text NOT NULL DEFAULT '',
  links                jsonb NOT NULL DEFAULT '[]', -- [{"kind": "bandcamp", "url": "https://…"}]
  public               boolean NOT NULL DEFAULT false,
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX albums_band_title_kind ON albums (band_id, lower(title), kind);

-- An album's artwork, uploaded (brand.PrepareArtwork: square, at most
-- 1200 px, JPEG), served at /album-covers/{album}/{hash}.jpg.
CREATE TABLE album_covers (
  album_id             bigint PRIMARY KEY REFERENCES albums(id) ON DELETE CASCADE,
  content_type         text NOT NULL,
  width                int NOT NULL,
  height               int NOT NULL,
  byte_size            int NOT NULL,
  sha256               text NOT NULL,
  data                 bytea NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL
);

-- Its tracks, in order: the band's song (its album version, never a live
-- version), the title as printed, and its master in the band's folder.
CREATE TABLE album_tracks (
  id           bigserial PRIMARY KEY,
  album_id     bigint NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
  disc         int NOT NULL DEFAULT 1 CHECK (disc >= 1),
  position     int NOT NULL CHECK (position >= 1),
  song_id      bigint REFERENCES songs(id) ON DELETE SET NULL,
  title        text NOT NULL CHECK (btrim(title) <> ''),
  duration_s   int CHECK (duration_s IS NULL OR duration_s > 0),
  tempo        int CHECK (tempo IS NULL OR tempo BETWEEN 20 AND 400),
  isrc         text NOT NULL DEFAULT '',
  recording_id bigint REFERENCES band_recordings(id) ON DELETE SET NULL,
  link_url     text NOT NULL DEFAULT '',
  UNIQUE (album_id, disc, position) DEFERRABLE INITIALLY IMMEDIATE
);
CREATE INDEX album_tracks_song_idx ON album_tracks (song_id) WHERE song_id IS NOT NULL;
CREATE INDEX album_tracks_recording_idx ON album_tracks (recording_id) WHERE recording_id IS NOT NULL;

-- The live version: a version (forked_from_id) of an album song, marked
-- live; one per song and owner.
ALTER TABLE songs ADD COLUMN live boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX songs_one_live_version ON songs (forked_from_id, owner_kind,
  coalesce(owner_person_id, owner_band_id, owner_community_id)) WHERE live;

-- The band's choice for its set lists: the song itself, or its live
-- version when there is one.
ALTER TABLE bands ADD COLUMN setlist_versions text NOT NULL DEFAULT 'song'
  CHECK (setlist_versions IN ('song', 'live'));

-- What's there moves in: one album per band_albums row (its year, its
-- credit; its hotlinked cover_url is not kept: artwork is uploaded now),
-- and one per distinct works.album of a band's songs with none; a track
-- per song, numbered by title (as the songbook showed them) until the
-- band orders them.
INSERT INTO albums (band_id, title, release_year, credits)
SELECT band_id, btrim(album), release_year, credit FROM band_albums WHERE btrim(album) <> ''
ON CONFLICT DO NOTHING;

WITH band_song AS (
  SELECT DISTINCT b.id AS band_id, s.id AS song_id, s.title, btrim(w.album) AS album
  FROM bands b
  JOIN songs s ON s.owner_band_id = b.id
            OR s.id IN (SELECT se.variant_id FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
                        WHERE sb.owner_band_id = b.id AND sb.is_default)
  JOIN works w ON w.id = s.work_id
  WHERE coalesce(btrim(w.album), '') <> ''
)
INSERT INTO albums (band_id, title)
SELECT DISTINCT bs.band_id, bs.album FROM band_song bs
WHERE NOT EXISTS (SELECT 1 FROM albums a WHERE a.band_id = bs.band_id AND lower(a.title) = lower(bs.album))
ON CONFLICT DO NOTHING;

WITH band_song AS (
  SELECT DISTINCT b.id AS band_id, s.id AS song_id, s.title, btrim(w.album) AS album
  FROM bands b
  JOIN songs s ON s.owner_band_id = b.id
            OR s.id IN (SELECT se.variant_id FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
                        WHERE sb.owner_band_id = b.id AND sb.is_default)
  JOIN works w ON w.id = s.work_id
  WHERE coalesce(btrim(w.album), '') <> ''
)
INSERT INTO album_tracks (album_id, position, song_id, title)
SELECT a.id, row_number() OVER (PARTITION BY a.id ORDER BY bs.title, bs.song_id), bs.song_id, bs.title
FROM band_song bs JOIN albums a ON a.band_id = bs.band_id AND lower(a.title) = lower(bs.album) AND a.kind = 'album';
