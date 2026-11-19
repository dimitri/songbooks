-- Warm-ups (docs/coaching.md, "Warm-ups"): a coaching community's
-- exercises, each a pattern the app writes out in every key (warmup.js:
-- a MusicXML score, the line to sing and the piano), shown once by the
-- coach's recording in its written key; each student's path of keys;
-- sessions, exercises played through as one.
CREATE TABLE exercises (
  id           bigserial PRIMARY KEY,
  community_id bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  name         text NOT NULL,
  kind         text NOT NULL DEFAULT 'pitch' CHECK (kind IN ('pitch', 'rhythm')),
  pattern      jsonb NOT NULL,
  notes        text NOT NULL DEFAULT '',
  position     int NOT NULL DEFAULT 0,
  -- its own level and words, over the community's (check_settings.go)
  level        text CHECK (level IN ('beginner', 'intermediate', 'advanced')),
  messages     jsonb NOT NULL DEFAULT '{}',
  -- the coach's recording, in the written key: to listen to, not to train with
  demo         bytea,
  demo_type    text NOT NULL DEFAULT '',
  demo_at      timestamptz,
  created_by   bigint REFERENCES people(id) ON DELETE SET NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX exercises_community_idx ON exercises (community_id, position, id);

-- A student's keys through an exercise: semitones from the written key,
-- waypoints walked a semitone a repeat; none, the app's (from their range).
CREATE TABLE exercise_paths (
  exercise_id bigint NOT NULL REFERENCES exercises(id) ON DELETE CASCADE,
  person_id   bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  waypoints   int[] NOT NULL,
  PRIMARY KEY (exercise_id, person_id)
);

-- A session: exercises in order, played through as one.
CREATE TABLE exercise_sessions (
  id           bigserial PRIMARY KEY,
  community_id bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  name         text NOT NULL,
  exercise_ids bigint[] NOT NULL DEFAULT '{}',
  position     int NOT NULL DEFAULT 0,
  created_at   timestamptz NOT NULL DEFAULT now()
);

-- Takes, practice and assignments of an exercise, as of a song.
ALTER TABLE practice_takes ALTER COLUMN variant_id DROP NOT NULL;
ALTER TABLE practice_takes ADD COLUMN exercise_id bigint REFERENCES exercises(id) ON DELETE CASCADE;
ALTER TABLE practice_takes ADD CONSTRAINT practice_takes_what CHECK ((variant_id IS NULL) <> (exercise_id IS NULL));
CREATE INDEX practice_takes_person_exercise ON practice_takes (person_id, exercise_id, created_at) WHERE exercise_id IS NOT NULL;

ALTER TABLE practice_logs ADD COLUMN exercise_id bigint REFERENCES exercises(id) ON DELETE SET NULL;
CREATE UNIQUE INDEX practice_logs_player_exercise_day ON practice_logs (person_id, community_id, exercise_id, practiced_on)
  WHERE source = 'player' AND exercise_id IS NOT NULL;

ALTER TABLE practice_assignments ADD COLUMN exercise_id bigint REFERENCES exercises(id) ON DELETE CASCADE;
ALTER TABLE practice_assignments DROP CONSTRAINT IF EXISTS practice_assignments_check1;
ALTER TABLE practice_assignments ADD CONSTRAINT practice_assignments_what
  CHECK (variant_id IS NOT NULL OR exercise_id IS NOT NULL OR title <> '');
