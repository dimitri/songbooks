-- A coaching student reads their voice's assessment and its history
-- (docs/coaching.md, "Voice types"): no switch for it any more.
-- And two kinds of notes about a student ("Notes"): private, the coach's
-- and the staff's only (every note so far: they were "only the coach
-- sees them"), and shared, the student reads them. A note goes from
-- private to shared (shared_at), never back. A note may be about a
-- lesson (event_id, 0080) or a song (variant_id).
-- (0108 is coach-books' voice types; the highest in review.)
ALTER TABLE student_voice_profiles DROP COLUMN shared;
ALTER TABLE teaching_notes ADD COLUMN shared boolean NOT NULL DEFAULT false;
ALTER TABLE teaching_notes ADD COLUMN shared_at timestamptz;
ALTER TABLE teaching_notes ADD COLUMN variant_id bigint REFERENCES songs(id) ON DELETE SET NULL;
