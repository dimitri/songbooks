-- A coaching student's voice type (docs/coaching.md, "Voice types"): the
-- coach's assessment (a type, or none yet, and a few words), the
-- student's comfortable part of their range (the tessitura; the range
-- itself is the registers' of 0079, student_voice_ranges), and whether
-- the student reads the assessment and its history. Every change to the
-- type, the tessitura, the assessment or a register's range is kept in
-- the history: when, who, the old value, the new one, the note.
-- (0107 is coach-books' song requests; the highest in review.)
CREATE TABLE student_voice_profiles (
  community_id         bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  person_id            bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  voice_type           text NOT NULL DEFAULT '' CHECK (voice_type IN ('', 'soprano', 'mezzo', 'contralto', 'countertenor', 'tenor', 'baritone', 'bass')),
  assessment           text NOT NULL DEFAULT '',
  tessitura_low_midi   int CHECK (tessitura_low_midi BETWEEN 0 AND 127),
  tessitura_high_midi  int CHECK (tessitura_high_midi BETWEEN 0 AND 127),
  shared               boolean NOT NULL DEFAULT false,
  updated_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  updated_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (community_id, person_id)
);

CREATE TABLE student_voice_history (
  id                   bigserial PRIMARY KEY,
  community_id         bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  person_id            bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  changed_at           timestamptz NOT NULL DEFAULT now(),
  changed_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  -- 'voice_type', 'tessitura', 'assessment', or a register: 'chest',
  -- 'mixed', 'head'. A range is "low-high" in MIDI notes, "" for none.
  field                text NOT NULL,
  old_value            text NOT NULL DEFAULT '',
  new_value            text NOT NULL DEFAULT '',
  note                 text NOT NULL DEFAULT ''
);
CREATE INDEX student_voice_history_student ON student_voice_history (community_id, person_id, changed_at DESC, id DESC);
