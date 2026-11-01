-- Phase 7: band proposals (new song / new reference URL) with a
-- yes/veto voting mechanism. See internal/store/band_proposals.go for the
-- accept/reject/vote logic and the judgment calls (auto-accept threshold,
-- veto semantics, notification policy) documented there.
--
-- target_variant_id is NOT NULL in practice for both kinds: a 'new_song'
-- proposal always references a real catalog variant id (the brief's
-- simplification over the originally-sketched "nullable, new_reference
-- only" shape — proposing a genuinely new original goes through the
-- band's own from-scratch creation flow first, then THAT variant gets
-- proposed for formal repertoire inclusion); a 'new_reference' proposal
-- references the existing band variant the proposed URL is for. Kept
-- nullable at the schema level only as a defensive matter, not because
-- any current code path leaves it unset.
CREATE TYPE band_proposal_kind AS ENUM ('new_song', 'new_reference');
CREATE TYPE band_proposal_status AS ENUM ('open', 'accepted', 'rejected');

CREATE TABLE band_proposals (
  id                    bigserial PRIMARY KEY,
  band_id               bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  proposer_person_id    bigint NOT NULL REFERENCES people(id),
  kind                  band_proposal_kind NOT NULL,
  target_variant_id     bigint REFERENCES songs(id) ON DELETE CASCADE,
  payload               jsonb NOT NULL DEFAULT '{}',
  status                band_proposal_status NOT NULL DEFAULT 'open',
  created_at            timestamptz NOT NULL DEFAULT now(),
  decided_at            timestamptz,
  decided_by_person_id  bigint REFERENCES people(id)
);
CREATE INDEX band_proposals_band_idx ON band_proposals (band_id, created_at DESC);
CREATE INDEX band_proposals_open_idx ON band_proposals (band_id, kind, status);

CREATE TYPE proposal_vote_kind AS ENUM ('yes', 'veto');

-- One vote per person per proposal; re-voting (yes <-> veto) upserts in
-- place rather than keeping a history — unlike band_members, there's no
-- "who voted what over time" story worth preserving here, just "what is
-- everyone's CURRENT vote right now."
CREATE TABLE proposal_votes (
  proposal_id bigint NOT NULL REFERENCES band_proposals(id) ON DELETE CASCADE,
  person_id   bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  vote        proposal_vote_kind NOT NULL,
  cast_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (proposal_id, person_id)
);
