-- Continues 0042: 'school' was added to the setlist_scope enum there
-- (its own transaction, a Postgres requirement — an enum value can't be
-- added and used in the same transaction), used here.
--
-- 0042 added songbooks.owner_school_id but missed the table's own two
-- CHECK constraints (owner_kind IN ('person','band'), and the mutual-
-- exclusivity check) — neither knew 'school' existed, so a school-owned
-- songbook insert failed outright. Caught by dry-running this migration
-- against a restored copy of production before deploying, not by any
-- Go-level check (a CHECK constraint violation has no compile-time
-- signal). Also adds the default-songbook-per-school uniqueness index
-- and an owner lookup index, mirroring person/band's own.
ALTER TABLE songbooks DROP CONSTRAINT songbooks_owner_kind_check;
ALTER TABLE songbooks ADD CONSTRAINT songbooks_owner_kind_check CHECK (owner_kind IN ('person', 'band', 'school'));
ALTER TABLE songbooks DROP CONSTRAINT songbooks_check;
ALTER TABLE songbooks ADD CONSTRAINT songbooks_check CHECK (
  (owner_kind = 'person' AND owner_person_id IS NOT NULL AND owner_band_id IS NULL AND owner_school_id IS NULL) OR
  (owner_kind = 'band'   AND owner_band_id IS NOT NULL   AND owner_person_id IS NULL AND owner_school_id IS NULL) OR
  (owner_kind = 'school' AND owner_school_id IS NOT NULL AND owner_person_id IS NULL AND owner_band_id IS NULL)
);
CREATE UNIQUE INDEX songbooks_default_school_idx ON songbooks (owner_school_id) WHERE is_default AND owner_kind = 'school';
CREATE INDEX songbooks_owner_school_idx ON songbooks (owner_school_id) WHERE owner_school_id IS NOT NULL;

ALTER TABLE setlists ADD COLUMN owner_school_id bigint REFERENCES schools(id) ON DELETE CASCADE;
ALTER TABLE setlists DROP CONSTRAINT setlists_scope_owner_check;
ALTER TABLE setlists ADD CONSTRAINT setlists_scope_owner_check CHECK (
  (scope = 'person' AND owner_person_id IS NOT NULL AND owner_band_id IS NULL AND owner_school_id IS NULL) OR
  (scope = 'band'   AND owner_band_id IS NOT NULL   AND owner_person_id IS NULL AND owner_school_id IS NULL) OR
  (scope = 'school' AND owner_school_id IS NOT NULL AND owner_person_id IS NULL AND owner_band_id IS NULL)
);

-- The community (school-target) invite link's role is chosen when the
-- link is minted (a "staff" link vs. a "member" link), not per named
-- roster slot the way a band's group invite works — default_status
-- carries that fixed role for any claim (named-slot or "someone else")
-- made through this invite. NULL preserves every existing band/school
-- group invite's current hardcoded behavior exactly (band: 'guest' for
-- an unlisted claim, already per-slot otherwise; school: 'student') —
-- this column only ever changes behavior when explicitly set, see
-- grantGroupInviteMembershipTx's updated doc comment.
ALTER TABLE group_invites ADD COLUMN default_status text;
