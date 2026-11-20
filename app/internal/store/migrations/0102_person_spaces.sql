-- The space switcher (menu PR 7): for each person, the bands and
-- communities they used last and the ones they pinned. The desktop
-- sidebar's "My groups" list puts the pinned ones first, then the last
-- used; the phone's Groups tab reopens the last used one. One row per
-- (person, space), upserted on every page view inside the space. A
-- space is a band or a community: one of band_id, community_id.
CREATE TABLE person_spaces (
  person_id    bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  band_id      bigint REFERENCES bands(id) ON DELETE CASCADE,
  community_id bigint REFERENCES communities(id) ON DELETE CASCADE,
  used_at      timestamptz,
  pinned_at    timestamptz,
  CHECK (num_nonnulls(band_id, community_id) = 1)
);
CREATE UNIQUE INDEX person_spaces_band ON person_spaces (person_id, band_id) WHERE band_id IS NOT NULL;
CREATE UNIQUE INDEX person_spaces_community ON person_spaces (person_id, community_id) WHERE community_id IS NOT NULL;
