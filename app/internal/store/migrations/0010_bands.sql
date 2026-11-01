-- Phase 2: bands. A band is a lightweight group of people who collaborate
-- on shared ("band"-visibility, added in the next migration) catalog
-- material. No band "owner"/"admin" role exists — see
-- internal/store/bands.go: any current member may add/remove/promote/demote
-- another member, and any current member may delete the whole band.
CREATE TABLE bands (
  id         bigserial PRIMARY KEY,
  name       text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE band_member_status AS ENUM ('core', 'guest');

-- History-preserving membership: every row ever inserted is kept forever
-- (never deleted, never reused). Current membership = status set AND
-- left_at IS NULL. Re-joining after leaving inserts a NEW row rather than
-- clearing left_at on the old one, so a person's full tenure in a band
-- (join, leave, rejoin) stays legible from the row history — deliberate,
-- don't "simplify" this into a single mutable row per (band, person).
--
-- Guests get identical edit rights to shared band material as core
-- members (see CanEditVariant in catalog.go) — status isn't a permission
-- tier, just a descriptive label (e.g. "regular member" vs. "sitting in").
CREATE TABLE band_members (
  id                   bigserial PRIMARY KEY,
  band_id              bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  person_id            bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  status               band_member_status NOT NULL,
  joined_at            timestamptz NOT NULL DEFAULT now(),
  left_at              timestamptz,
  invited_by_person_id bigint REFERENCES people(id)
);
CREATE INDEX band_members_band_idx ON band_members (band_id, left_at);
CREATE INDEX band_members_person_idx ON band_members (person_id, left_at);
-- At most one CURRENT (left_at IS NULL) row per (band, person) — re-joining
-- must insert a fresh row only once the previous one has left_at set,
-- never two simultaneously-current rows for the same pair.
CREATE UNIQUE INDEX band_members_current_unique ON band_members (band_id, person_id) WHERE left_at IS NULL;
