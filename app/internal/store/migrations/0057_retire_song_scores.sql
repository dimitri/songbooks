-- Retires song_scores (migration 0055), introduced in the same change
-- that's dropping it here: a section's note-level "score:" source is
-- plain text in the song body itself now ("score[label]: ..." —
-- song.ScoreVoice), not a separate row keyed by section index. Single
-- file, single source of truth: a .song document (including its
-- notation) is meant to be readable, editable and git-diffable on its
-- own, with no out-of-band database lookup needed to reconstruct it —
-- the goal behind a planned standalone CLI (source file in, PDF out,
-- no database at all).
DROP TABLE song_scores;
