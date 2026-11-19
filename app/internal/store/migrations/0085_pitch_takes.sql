-- Checking one's singing (pitch-check.js): every take checked, kept for
-- the progress; the day's practice of a song keeps how many and the last
-- scores; an assignment gets the coach's goal (a level, and the share of
-- notes in tune and on time to reach) and when it was reached.
CREATE TABLE practice_takes (
  id           bigserial PRIMARY KEY,
  person_id    bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  community_id bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  variant_id   bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  songbook_id  bigint REFERENCES songbooks(id) ON DELETE SET NULL,
  level        text NOT NULL CHECK (level IN ('beginner', 'intermediate', 'advanced')),
  pitch        int NOT NULL CHECK (pitch BETWEEN 0 AND 100),   -- % of the notes sung, in tune
  rhythm       int NOT NULL CHECK (rhythm BETWEEN 0 AND 100),  -- % of them, on time
  cents        int NOT NULL DEFAULT 0,  -- the tendency, flat below 0
  ms           int NOT NULL DEFAULT 0,  -- early below 0
  notes        int NOT NULL DEFAULT 0,  -- notes sung
  hits         int NOT NULL DEFAULT 0,  -- in tune
  seconds      int NOT NULL DEFAULT 0,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX practice_takes_person_song ON practice_takes (person_id, variant_id, created_at);

ALTER TABLE practice_logs ADD COLUMN pitch_takes int NOT NULL DEFAULT 0;
ALTER TABLE practice_logs ADD COLUMN pitch_score int CHECK (pitch_score BETWEEN 0 AND 100);
ALTER TABLE practice_logs ADD COLUMN rhythm_score int CHECK (rhythm_score BETWEEN 0 AND 100);

ALTER TABLE practice_assignments ADD COLUMN level text CHECK (level IN ('beginner', 'intermediate', 'advanced'));
ALTER TABLE practice_assignments ADD COLUMN pitch_target int CHECK (pitch_target BETWEEN 1 AND 100);
ALTER TABLE practice_assignments ADD COLUMN rhythm_target int CHECK (rhythm_target BETWEEN 1 AND 100);
ALTER TABLE practice_assignments ADD COLUMN reached_at timestamptz;
