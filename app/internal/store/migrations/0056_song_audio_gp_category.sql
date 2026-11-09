-- A third reference-audio category: the backing track embedded in an
-- imported Guitar Pro file (see internal/gpimport) — already synced to
-- the song's own bars via the GP file's own per-bar SyncPoint data (see
-- gp_bar_sync_points below), unlike a plain YouTube/band recording whose
-- section markers are tapped in by hand. Listening defaults to this one
-- when present (set client-side, same "personal choice, not synced"
-- posture the youtube/band split already has for the other two).
ALTER TABLE song_audio DROP CONSTRAINT song_audio_category_check;
ALTER TABLE song_audio ADD CONSTRAINT song_audio_category_check CHECK (category IN ('youtube', 'band', 'gp'));

-- Per-BAR playback sync, finer-grained than song_audio_marker's per-
-- SECTION markers above — only ever populated from a GP import's own
-- SyncPoint automations (real audio-frame offsets GP computed from the
-- actual recording, bar by bar, not interpolated). bar_index is the
-- song's own REAL performed bar numbering (realBarCount's output,
-- handlers_pages.go) — the same 0-based sequence a chart's bars render
-- in, repeats/voltas already expanded — so a click on any rendered bar
-- maps straight to one row here with no further unpacking needed.
CREATE TABLE gp_bar_sync_point (
  song_audio_id bigint NOT NULL REFERENCES song_audio(id) ON DELETE CASCADE,
  bar_index integer NOT NULL,
  time_seconds double precision NOT NULL,
  PRIMARY KEY (song_audio_id, bar_index)
);
