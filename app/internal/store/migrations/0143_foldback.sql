-- The edit model, step 6 (docs/catalogue-edit-review.md §4.7): fold back
-- the arrangements of The Big Songbook's songs that are only fixes. An
-- arrangement identical to its original, or differing by a fix, is
-- offered to fold back: "Use the shared chart" / "Send the fix and use
-- the shared chart" (store/foldback.go: its songbook entries, set list
-- items, keys and notes move to the original, the arrangement goes, its
-- history kept and listed on the original's, song_merges reason
-- 'foldback'), or "It's our arrangement — keep it", which this keeps:
-- the offer isn't made again.
ALTER TABLE songs
  ADD COLUMN foldback_kept_at timestamptz,
  ADD COLUMN foldback_kept_by bigint REFERENCES people(id) ON DELETE SET NULL;
