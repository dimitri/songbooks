-- Practice (docs/coaching.md, "Practice"): what the coach gives a student
-- or a group to work on between lessons, and what each student practised.

-- An assignment: a song (or later an exercise) of the community, or
-- something written ("lip trills, five minutes"), for a student or a
-- group, how many days a week, until when.
CREATE TABLE practice_assignments (
  id           bigserial PRIMARY KEY,
  community_id bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  person_id    bigint REFERENCES people(id) ON DELETE CASCADE,
  group_id     bigint REFERENCES coaching_groups(id) ON DELETE CASCADE,
  variant_id   bigint REFERENCES songs(id) ON DELETE CASCADE,
  title        text NOT NULL DEFAULT '',
  notes        text NOT NULL DEFAULT '',
  per_week     int NOT NULL DEFAULT 0 CHECK (per_week BETWEEN 0 AND 7),
  due_on       date,
  created_by   bigint REFERENCES people(id) ON DELETE SET NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  done_at      timestamptz,
  CHECK ((person_id IS NULL) <> (group_id IS NULL)),
  CHECK (variant_id IS NOT NULL OR title <> '')
);
CREATE INDEX practice_assignments_person_idx ON practice_assignments (person_id) WHERE person_id IS NOT NULL;
CREATE INDEX practice_assignments_group_idx ON practice_assignments (group_id) WHERE group_id IS NOT NULL;

-- A day's practice: played in the app (the song page counts the time a
-- recording plays, one row a song a day) or told by the student.
CREATE TABLE practice_logs (
  id            bigserial PRIMARY KEY,
  person_id     bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  community_id  bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  assignment_id bigint REFERENCES practice_assignments(id) ON DELETE SET NULL,
  variant_id    bigint REFERENCES songs(id) ON DELETE SET NULL,
  practiced_on  date NOT NULL,
  seconds       int NOT NULL DEFAULT 0 CHECK (seconds >= 0),
  source        text NOT NULL CHECK (source IN ('player', 'manual')),
  note          text NOT NULL DEFAULT '',
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX practice_logs_person_idx ON practice_logs (community_id, person_id, practiced_on DESC);
CREATE UNIQUE INDEX practice_logs_player_day ON practice_logs (person_id, community_id, variant_id, practiced_on) WHERE source = 'player';
