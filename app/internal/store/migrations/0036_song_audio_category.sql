-- Splits song_audio into two reference categories per song, not one —
-- "a personal choice of reference version (yt video, band recording: two
-- categories only)" — a YouTube-sourced download and the band's own
-- practice/live recording are different things a listener might want,
-- not competing "latest upload wins" takes of the same reference.
ALTER TABLE song_audio ADD COLUMN category text NOT NULL DEFAULT 'band';
ALTER TABLE song_audio ADD CONSTRAINT song_audio_category_check CHECK (category IN ('youtube', 'band'));

DROP INDEX song_audio_variant_idx;
CREATE UNIQUE INDEX song_audio_variant_category_idx ON song_audio(variant_id, category);
