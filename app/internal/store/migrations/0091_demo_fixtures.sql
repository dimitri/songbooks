-- Demo fixtures (cmd/sbdemo, docs/demo-data.md): every row the fixtures
-- make, so `sbdemo remove` (or the admin page's button) takes them away
-- again and leaves the data as it was. Empty everywhere but on a demo copy.
CREATE TABLE demo_fixture_rows (
  kind       text NOT NULL,   -- person, membership, ranges, student_notes, songbook, teaching_note, group, event, setlist
  id         bigint NOT NULL, -- the row's id (a community's, for membership/ranges/student_notes)
  ref        bigint NOT NULL DEFAULT 0, -- the person, for membership/ranges/student_notes
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (kind, id, ref)
);
