-- How a band's recordings are named (docs/band-recordings.md): the band's
-- own name templates, read before the built-in ones; a recorder's own file
-- names (TRACK01, ZOOM0004, IMG_2602) marked raw, listed but never asked
-- about; and a correction for one file ('file': its id, to a song's id or
-- 'none', not a song).
ALTER TABLE band_recording_sources ADD COLUMN name_templates text[] NOT NULL DEFAULT '{}';
ALTER TABLE band_recordings ADD COLUMN raw boolean NOT NULL DEFAULT false;
ALTER TABLE band_recording_fixes DROP CONSTRAINT band_recording_fixes_kind_check;
ALTER TABLE band_recording_fixes ADD CONSTRAINT band_recording_fixes_kind_check CHECK (kind IN ('date', 'title', 'file'));
