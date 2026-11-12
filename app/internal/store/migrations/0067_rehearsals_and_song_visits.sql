-- Rehearsals are band events like any other (migration 0049), with the
-- set list to practise attached through events.setlist_id.
ALTER TYPE event_type ADD VALUE IF NOT EXISTS 'rehearsal';

-- The songs a person opened last, for the home page's "Continue": one row
-- per person and song, moved to the top on every visit. query is the
-- context the song was opened in ("band=15", "setlist=3"...), so going
-- back to it lands in the same list with the same previous/next.
CREATE TABLE song_visits (
  person_id  bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  song_id    bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  query      text NOT NULL DEFAULT '',
  visited_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (person_id, song_id)
);
CREATE INDEX song_visits_recent ON song_visits (person_id, visited_at DESC);
