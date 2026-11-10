-- Band member-units of a community (JamSpace onboarding): a band can be
-- linked to a community as a unit, so that every current member of a
-- linked band is also granted ordinary individual community membership
-- (see community_bands.go). Built with the validity convention from day
-- one (migration 0059) — no legacy two-column baggage, since this table
-- is brand new.
CREATE TABLE community_band_members (
  id                   bigserial PRIMARY KEY,
  community_id         bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  band_id              bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  validity             tstzrange NOT NULL DEFAULT tstzrange(now(), null),
  invited_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX community_band_members_current_unique
  ON community_band_members (community_id, band_id) WHERE upper_inf(validity);
CREATE INDEX community_band_members_community_idx ON community_band_members (community_id);
CREATE INDEX community_band_members_band_idx ON community_band_members (band_id);

-- NULL = joined directly (manual add, self-join, or a community-targeted
-- group_invites claim); non-null names the band whose link caused the
-- auto-grant (see ensureCommunityMembershipForBandMember). Set once at
-- insert, never retroactively cleared — a later direct (re)join produces
-- its own fresh stint row with joined_via_band_id = NULL, same one-row-
-- per-stint convention every other membership table in this app follows.
ALTER TABLE community_members
  ADD COLUMN joined_via_band_id bigint REFERENCES bands(id) ON DELETE SET NULL;
