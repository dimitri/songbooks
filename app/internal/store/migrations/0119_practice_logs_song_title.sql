-- A deleted song's practice stays in the history (docs/ownership-model.md
-- §5, store.DeleteSong): a day's practice of it points at the frozen copy
-- made for that person when there is one; otherwise variant_id goes NULL
-- (ON DELETE SET NULL, migration 0084) and the row keeps the song's title
-- and artist as they were, so the history still reads "Wicked Game —
-- 20 min". '' while the song is there: its own title is read then.
ALTER TABLE practice_logs
  ADD COLUMN song_title text NOT NULL DEFAULT '',
  ADD COLUMN song_artist text NOT NULL DEFAULT '';
