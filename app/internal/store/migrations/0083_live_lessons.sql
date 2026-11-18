-- Live lessons (docs/live-lessons.md): an online lesson's room on Galène,
-- made at its first join, and whether a person joins wearing headphones
-- (their voice then sent as it is, for singing).
ALTER TABLE events ADD COLUMN live_room uuid UNIQUE;
ALTER TABLE people ADD COLUMN live_headphones boolean NOT NULL DEFAULT false;
