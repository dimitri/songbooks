-- A lesson's video (docs/courses.md): the author's lesson on YouTube,
-- which a course's playlist plays and the lesson's page shows. Empty: the
-- first YouTube link its songs have.
ALTER TABLE lessons ADD COLUMN video_url text NOT NULL DEFAULT '';
