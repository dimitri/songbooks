-- A songbook can be a course (docs/courses.md): the same product, with the
-- same access, sharing and downloads, whose songs are grouped in lessons.
-- A lesson has the author's number, a title, an optional text, its songs
-- (songbook_entries.lesson_id, in lesson_position order) and its files
-- (a file goes with its song's lesson, or with a lesson directly when it
-- has no song: songbook_files.lesson_id). Lessons are in number order,
-- unnumbered ones last.
--
-- Progress: a member has started a lesson once they've opened one of its
-- songs or files (song_visits, songbook_downloads); done is theirs to say
-- (lesson_progress).
ALTER TABLE songbooks ADD COLUMN kind text NOT NULL DEFAULT 'songbook' CHECK (kind IN ('songbook', 'course'));

CREATE TABLE lessons (
  id          bigserial PRIMARY KEY,
  songbook_id bigint NOT NULL REFERENCES songbooks(id) ON DELETE CASCADE,
  number      int,
  title       text NOT NULL,
  body        text NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX lessons_songbook_idx ON lessons (songbook_id, number);

ALTER TABLE songbook_entries
  ADD COLUMN lesson_id bigint REFERENCES lessons(id) ON DELETE SET NULL,
  ADD COLUMN lesson_position int NOT NULL DEFAULT 0;
CREATE INDEX songbook_entries_lesson_idx ON songbook_entries (lesson_id) WHERE lesson_id IS NOT NULL;

ALTER TABLE songbook_files ADD COLUMN lesson_id bigint REFERENCES lessons(id) ON DELETE SET NULL;

CREATE TABLE lesson_progress (
  lesson_id bigint NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
  person_id bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  done_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (lesson_id, person_id)
);
CREATE INDEX lesson_progress_person_idx ON lesson_progress (person_id);
