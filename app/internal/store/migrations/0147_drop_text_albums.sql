-- The text albums are replaced by albums and album_tracks (0146): nothing
-- reads works.album or band_albums any more.
DROP TABLE band_albums;
ALTER TABLE works DROP COLUMN album;
