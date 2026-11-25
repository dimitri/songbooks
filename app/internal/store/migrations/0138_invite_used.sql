-- A single-use invite (a personal link, a plain signup one) is kept once
-- taken, as a tombstone, instead of being deleted: someone who opens the
-- link again (to come back in, the day after joining) is told it was
-- used, and where it took them, and to log in; the admin's lists show it
-- as used, not open nor gone.
--
--   * used_at: when it was taken; NULL while it is still open. Every way
--     of taking or showing an open invite reads used_at IS NULL, so a
--     used link can never be taken again;
--   * used_by_person_id: the account that took it;
--   * joined_band_id / joined_community_id: where a personal link took
--     them (the placeholder's first band, else first community: the page
--     it landed on), for the used link's page to name it. At most one.

ALTER TABLE invites
  ADD COLUMN used_at timestamptz,
  ADD COLUMN used_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  ADD COLUMN joined_band_id bigint REFERENCES bands(id) ON DELETE SET NULL,
  ADD COLUMN joined_community_id bigint REFERENCES communities(id) ON DELETE SET NULL;
