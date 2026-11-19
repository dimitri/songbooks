-- A coach's own settings for checking singing (pitch-check.js): the three
-- levels (a name, the margin in cents to be in tune, in ms to be on time)
-- and the words a student gets after a take, in their voice. Empty: the
-- app's. A take keeps the margins it was judged with.
CREATE TABLE coaching_check_settings (
  community_id bigint PRIMARY KEY REFERENCES communities(id) ON DELETE CASCADE,
  levels       jsonb NOT NULL DEFAULT '{}',
  messages     jsonb NOT NULL DEFAULT '{}',
  updated_at   timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE practice_takes ADD COLUMN cents_margin int;
ALTER TABLE practice_takes ADD COLUMN ms_margin int;
