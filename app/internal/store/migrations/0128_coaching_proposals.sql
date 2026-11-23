-- A student proposes a song of their own to the coach (docs/coaching.md,
-- "A student proposes a song"): a song from their own songbook, one they
-- found or made, not the coach's repertoire. It waits with the "Ask for
-- it" requests (0107, one row per student and song), told apart by kind:
-- 'ask' (a song of the repertoire) or 'propose' (the student's). A
-- proposal carries the student's note and the key they'd like (semitones
-- from the proposed song's own key, NULL: none said); the coach answers
-- with a note of their own (reply), and an accepted proposal says which
-- song entered the repertoire (result_variant_id: the proposed song when
-- it's public, linked; the community's arrangement made from it when
-- it was the student's private song). (0127 is band-votes'.)
ALTER TABLE coaching_song_requests
  ADD COLUMN kind text NOT NULL DEFAULT 'ask' CHECK (kind IN ('ask', 'propose')),
  ADD COLUMN note text NOT NULL DEFAULT '',
  ADD COLUMN wished_semitones int CHECK (wished_semitones BETWEEN -12 AND 12),
  ADD COLUMN reply text NOT NULL DEFAULT '',
  ADD COLUMN result_variant_id bigint REFERENCES songs(id) ON DELETE SET NULL;

-- The coach sees a proposed song while the proposal waits (store
-- SongRights.ViaProposal): found by song.
CREATE INDEX coaching_song_requests_proposed ON coaching_song_requests (variant_id) WHERE kind = 'propose' AND status = 'pending';
