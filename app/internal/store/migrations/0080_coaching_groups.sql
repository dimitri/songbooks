-- A coaching community's groups, lessons in the room or online, teaching
-- notes, and everyone's calendar feed (docs/coaching.md).

-- A group of students: a year's class (September to June, say), or a
-- one-off around an event (a master class, a "stage"). Its songs are its
-- songbook (kind "group", gated to its members), each in the group's key.
CREATE TABLE coaching_groups (
  id bigserial PRIMARY KEY,
  community_id bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  name text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('year', 'oneoff')),
  starts_on date,
  ends_on date,
  event_id bigint REFERENCES events(id) ON DELETE SET NULL,
  songbook_id bigint REFERENCES songbooks(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX coaching_groups_community_idx ON coaching_groups (community_id);

CREATE TABLE coaching_group_members (
  group_id bigint NOT NULL REFERENCES coaching_groups(id) ON DELETE CASCADE,
  person_id bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  added_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (group_id, person_id)
);

ALTER TABLE songbooks DROP CONSTRAINT songbooks_kind_check;
ALTER TABLE songbooks ADD CONSTRAINT songbooks_kind_check CHECK (kind IN ('songbook', 'course', 'student', 'group'));

-- A lesson is in the room or online, for students one by one or a group.
ALTER TABLE events ADD COLUMN lesson_mode text CHECK (lesson_mode IN ('in_person', 'online'));
ALTER TABLE events ADD COLUMN coaching_group_id bigint REFERENCES coaching_groups(id) ON DELETE SET NULL;

-- What the coach writes about a student, dated, and about which lesson.
CREATE TABLE teaching_notes (
  id bigserial PRIMARY KEY,
  community_id bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  person_id bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  event_id bigint REFERENCES events(id) ON DELETE SET NULL,
  body text NOT NULL,
  author_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX teaching_notes_student_idx ON teaching_notes (community_id, person_id, created_at DESC);

-- A person's calendar as a feed (iCal), at a secret address of theirs.
CREATE TABLE calendar_feeds (
  person_id bigint PRIMARY KEY REFERENCES people(id) ON DELETE CASCADE,
  token text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- A master class, a weekend "stage": a community's event, with its group.
ALTER TYPE event_type ADD VALUE IF NOT EXISTS 'masterclass';
