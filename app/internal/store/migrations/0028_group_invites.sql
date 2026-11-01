-- Group invites: one shareable link that pre-names an expected roster
-- (band members or school students/instructors-by-default) and lets each
-- named person — or an unlisted "someone else" — claim a spot by either
-- creating a brand-new account or logging into an existing one. Distinct
-- from the single-use `invites` table (one token = one accept = consumed):
-- a group invite's token stays reusable, each SLOT within it claimable
-- exactly once (see group_invite_slots.claimed_by_person_id).
--
-- target_kind + exactly one of band_id/school_id mirrors the
-- invites_one_target CHECK pattern from migration 0019.
CREATE TABLE group_invites (
  id                   bigserial PRIMARY KEY,
  token                text UNIQUE NOT NULL,
  target_kind          text NOT NULL CHECK (target_kind IN ('band', 'school')),
  band_id              bigint REFERENCES bands(id) ON DELETE CASCADE,
  school_id            bigint REFERENCES schools(id) ON DELETE CASCADE,
  created_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  closed_at            timestamptz,
  CONSTRAINT group_invites_one_target CHECK (
    (target_kind = 'band'   AND band_id   IS NOT NULL AND school_id IS NULL) OR
    (target_kind = 'school' AND school_id IS NOT NULL AND band_id   IS NULL)
  )
);

-- One row per pre-typed expected person. instrument is only meaningful
-- for a band's roster (an instrument-free text label); a school roster
-- entry is just a name, since school slots default to 'student' on
-- claim (see store.ClaimGroupInviteSlot) rather than carrying a role.
-- claimed_by_person_id/claimed_at are both null until claimed, and a
-- claimed slot is never un-claimed (no "undo" — removing a resulting
-- membership is a separate, existing action on the band/school admin
-- page).
CREATE TABLE group_invite_slots (
  id                    bigserial PRIMARY KEY,
  group_invite_id       bigint NOT NULL REFERENCES group_invites(id) ON DELETE CASCADE,
  display_name          text NOT NULL,
  instrument            text,
  claimed_by_person_id  bigint REFERENCES people(id) ON DELETE SET NULL,
  claimed_at            timestamptz
);

CREATE INDEX group_invite_slots_invite_idx ON group_invite_slots(group_invite_id);
