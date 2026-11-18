-- A coaching community: a voice coach and their students (docs/coaching.md).
ALTER TABLE communities ADD COLUMN kind text NOT NULL DEFAULT 'community'
  CHECK (kind IN ('community', 'coaching'));

-- A student's songbook: the community's, kept by the coach for one of its
-- students (gated with a grant to that student, as a paid songbook is).
ALTER TABLE songbooks DROP CONSTRAINT songbooks_kind_check;
ALTER TABLE songbooks ADD CONSTRAINT songbooks_kind_check CHECK (kind IN ('songbook', 'course', 'student'));
ALTER TABLE songbooks ADD COLUMN student_person_id bigint REFERENCES people(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX songbooks_student_idx ON songbooks (owner_community_id, student_person_id)
  WHERE student_person_id IS NOT NULL;

-- A song's key in a songbook: the student's, from the reference recording.
ALTER TABLE songbook_entries ADD COLUMN semitones int NOT NULL DEFAULT 0
  CHECK (semitones BETWEEN -12 AND 12);

-- What the coach notes of a student's voice: a range per register, they
-- overlap (the mixed voice between chest and head), MIDI note numbers.
CREATE TABLE student_voice_ranges (
  community_id bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  person_id bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  register text NOT NULL CHECK (register IN ('chest', 'mixed', 'head')),
  low_midi int CHECK (low_midi BETWEEN 21 AND 108),
  high_midi int CHECK (high_midi BETWEEN 21 AND 108),
  note text NOT NULL DEFAULT '',
  updated_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (community_id, person_id, register)
);

-- The coach's notes on a student, free text.
CREATE TABLE student_notes (
  community_id bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  person_id bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  notes text NOT NULL DEFAULT '',
  updated_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (community_id, person_id)
);

-- A coaching lesson (its students are its registrations, put there by the
-- coach), and an open mic.
ALTER TYPE event_type ADD VALUE IF NOT EXISTS 'lesson';
ALTER TYPE event_type ADD VALUE IF NOT EXISTS 'open_mic';
