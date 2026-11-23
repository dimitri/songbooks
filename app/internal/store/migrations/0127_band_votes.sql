-- A band's votes on its proposals with stars (docs/band-votes.md): each
-- core member (guests don't vote) rates a proposed song 1 to 4 stars — an even scale, so a vote is
-- never right in the middle: 3 and 4 are for it, 1 and 2 against —, or
-- says "no thanks" (a refusal, not a star), or hasn't voted yet (no row:
-- never 0 stars). (0126 is the melody sheet's.)
--
-- The old yes/veto votes are kept as they were cast (legacy_vote) and
-- become: yes → 3 stars (for it, the plain "yes" it was: 4 is left for
-- "I love it", which a yes never said), veto → no thanks (the refusal
-- it was).
ALTER TABLE proposal_votes
  ADD COLUMN stars smallint CHECK (stars BETWEEN 1 AND 4),
  ADD COLUMN no_thanks boolean NOT NULL DEFAULT false,
  ADD COLUMN legacy_vote proposal_vote_kind;

UPDATE proposal_votes
   SET legacy_vote = vote,
       stars = CASE WHEN vote = 'yes' THEN 3 END,
       no_thanks = (vote = 'veto');

ALTER TABLE proposal_votes DROP COLUMN vote;
ALTER TABLE proposal_votes
  ADD CONSTRAINT proposal_votes_stars_or_no_thanks CHECK ((stars IS NULL) = no_thanks);

-- Every vote cast, changed or taken back, in order: proposal_votes is
-- what stands now, this is how it got there (the old votes first, as
-- they were cast). A row with neither stars nor no_thanks is a vote
-- taken back.
CREATE TABLE proposal_vote_log (
  id          bigserial PRIMARY KEY,
  proposal_id bigint NOT NULL REFERENCES band_proposals(id) ON DELETE CASCADE,
  person_id   bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  stars       smallint CHECK (stars BETWEEN 1 AND 4),
  no_thanks   boolean NOT NULL DEFAULT false,
  legacy_vote proposal_vote_kind,
  cast_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX proposal_vote_log_proposal_idx ON proposal_vote_log (proposal_id, cast_at);

INSERT INTO proposal_vote_log (proposal_id, person_id, stars, no_thanks, legacy_vote, cast_at)
SELECT proposal_id, person_id, stars, no_thanks, legacy_vote, cast_at
  FROM proposal_votes
 ORDER BY cast_at;

-- Only a band's core members vote now: a guest's vote (or a former
-- member's) no longer stands — it stays in the history above, out of
-- every average and count.
DELETE FROM proposal_votes v
 USING band_proposals bp
 WHERE bp.id = v.proposal_id
   AND NOT EXISTS (SELECT 1 FROM band_members bm
                    WHERE bm.band_id = bp.band_id AND bm.person_id = v.person_id
                      AND upper_inf(bm.validity) AND bm.status = 'core');

-- What a "no thanks" does, the band's choice (its settings):
--   block (the default): a core member's no thanks holds the song back:
--         it isn't accepted, by the votes or by hand, while it stands;
--   veto:  a core member's no thanks turns the song down at once (the
--          old veto);
--   count: no thanks only counts, as a refusal shown beside the stars.
-- Whatever the rule, more than half of the core members saying no
-- thanks turns a song down.
ALTER TABLE bands
  ADD COLUMN proposal_no_thanks text NOT NULL DEFAULT 'block'
  CHECK (proposal_no_thanks IN ('block', 'veto', 'count'));

-- A listening party (the event type, its page): what the band is
-- listening to together right now, as the one playing it (the host)
-- says, so each member's phone shows that song to rate.
CREATE TABLE listening_party_now (
  band_id        bigint PRIMARY KEY REFERENCES bands(id) ON DELETE CASCADE,
  proposal_id    bigint REFERENCES band_proposals(id) ON DELETE SET NULL,
  event_id       bigint REFERENCES events(id) ON DELETE SET NULL,
  host_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  playing        boolean NOT NULL DEFAULT false,
  updated_at     timestamptz NOT NULL DEFAULT now()
);
