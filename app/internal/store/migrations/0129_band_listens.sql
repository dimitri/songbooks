-- A band's votes help it decide, they never decide (docs/band-votes.md):
-- the setting of what a no thanks did (0127) goes, and so does the
-- listening party's "now playing" between devices (0127), which no one
-- wanted. In their place, two pools of songs to listen to, besides the
-- proposals to vote on: Discover (songs a member shares for the band to
-- hear, no vote) and Influences (the artists and songs that shape the
-- band's sound). (0128 is coaching's.)
ALTER TABLE bands DROP COLUMN proposal_no_thanks;
DROP TABLE listening_party_now;

CREATE TABLE band_listens (
  id                 bigserial PRIMARY KEY,
  band_id            bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  pool               text NOT NULL CHECK (pool IN ('discover', 'influence')),
  url                text NOT NULL,
  video_id           text NOT NULL DEFAULT '',
  title              text NOT NULL DEFAULT '',
  artist             text NOT NULL DEFAULT '',
  note               text NOT NULL DEFAULT '',
  tags               text[] NOT NULL DEFAULT '{}',
  added_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  -- A shared song proposed for a vote since (Discover's "Propose for a
  -- vote").
  proposal_id        bigint REFERENCES band_proposals(id) ON DELETE SET NULL
);
CREATE INDEX band_listens_band_idx ON band_listens (band_id, pool, created_at DESC);
