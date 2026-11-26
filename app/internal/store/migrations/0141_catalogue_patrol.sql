-- The edit model, step 3 (docs/catalogue-edit-review.md §4.5): patrol
-- instead of holding. Edits of The Big Songbook's songs by anyone not its
-- staff are applied at once (a newcomer's too) and checked afterwards by
-- its staff and trusted editors: "Recent edits to check" lists the
-- applied edits nobody has looked at yet, each marked seen ("Looks good")
-- or reverted. Held for review (pending) stay only the edits that touch
-- what identifies the song or lines it up with its recording (the key's
-- name, the lead-in, the Guitar Pro sync, removing the recording), an
-- account's very first edit of a song none of its places holds, and the
-- edits past the daily limit of someone not trusted yet.
--
-- patrolled_at/by: a reviewer saw the applied edit (or its author is
-- trusted: seen as made). reverted_at/by: the edit was undone (a
-- reviewer's Revert, or its revision reverted in History); an edit
-- reverted within 7 days doesn't count towards its author's trust.
-- reasons, on an applied edit, say why it's worth a look ('fit',
-- 'structure', 'newcomer').
ALTER TABLE catalogue_edits
  ADD COLUMN patrolled_at timestamptz,
  ADD COLUMN patrolled_by bigint REFERENCES people(id) ON DELETE SET NULL,
  ADD COLUMN reverted_at  timestamptz,
  ADD COLUMN reverted_by  bigint REFERENCES people(id) ON DELETE SET NULL;

-- The edits applied before: applied under the old rules (they kept the
-- song following its recording, by someone past their first edits) or
-- approved by a reviewer: seen.
UPDATE catalogue_edits SET patrolled_at = coalesce(decided_at, created_at)
WHERE status IN ('applied', 'approved');

CREATE INDEX catalogue_edits_to_check_idx ON catalogue_edits (created_at)
  WHERE status = 'applied' AND patrolled_at IS NULL AND reverted_at IS NULL;
CREATE INDEX catalogue_edits_revision_idx ON catalogue_edits (revision_id) WHERE revision_id IS NOT NULL;
