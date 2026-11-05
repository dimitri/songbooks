-- Finishes the school -> community rename at the data layer. Migration
-- 0042 deliberately kept every internal identifier (table/column names,
-- Go types) as "school", changing only user-facing copy — reasoned as
-- "zero rows today, no functional benefit to a full rename." That's no
-- longer true (real communities and real data now exist), and the user
-- explicitly asked for the complete rename this time, "including
-- internal symbols and comments" — this migration is the data-layer
-- half of that; the matching Go/template rename is a separate, non-SQL
-- change in the same commit.

-- Tables.
ALTER TABLE schools RENAME TO communities;
ALTER TABLE school_members RENAME TO community_members;
ALTER TABLE school_submissions RENAME TO community_submissions;

-- Columns — renaming the column updates every view/constraint/index
-- definition's reference to it automatically; only literal 'school'
-- *values* (enum labels, and the plain-text owner_kind/target_kind
-- columns below) need handling separately.
ALTER TABLE community_members RENAME COLUMN school_id TO community_id;
ALTER TABLE community_submissions RENAME COLUMN school_id TO community_id;
ALTER TABLE songs RENAME COLUMN owner_school_id TO owner_community_id;
ALTER TABLE group_invites RENAME COLUMN school_id TO community_id;
ALTER TABLE songbooks RENAME COLUMN owner_school_id TO owner_community_id;
ALTER TABLE setlists RENAME COLUMN owner_school_id TO owner_community_id;
ALTER TABLE events RENAME COLUMN owner_school_id TO owner_community_id;

-- Enum type name + enum value renames. Renaming an enum LABEL (not
-- just the type) is safe for existing rows and for any constraint/
-- index that compares against it by name — Postgres stores enum
-- comparisons by the type's internal OID/sort position, not the label
-- text, so this is a pure display-name change, no data rewrite needed.
ALTER TYPE school_member_role RENAME TO community_member_role;
ALTER TYPE school_submission_kind RENAME TO community_submission_kind;
ALTER TYPE school_submission_status RENAME TO community_submission_status;
ALTER TYPE setlist_scope RENAME VALUE 'school' TO 'community';
ALTER TYPE event_owner_kind RENAME VALUE 'school' TO 'community';
ALTER TYPE variant_visibility RENAME VALUE 'school' TO 'community';

-- songbooks.owner_kind and group_invites.target_kind are plain TEXT
-- columns with a CHECK, not enums — the 'school' value is real stored
-- data here, not just a type label. The old CHECK constraints (and the
-- partial-index predicate below) only know about the literal 'school',
-- not 'community' yet, so they must be dropped BEFORE the data UPDATE
-- runs, or the UPDATE itself violates them (caught when this migration
-- was first run against production, which — unlike the empty local
-- test database this was authored against — actually has school-owned
-- songbooks).
ALTER TABLE songbooks DROP CONSTRAINT songbooks_owner_kind_check;
ALTER TABLE songbooks DROP CONSTRAINT songbooks_check;
DROP INDEX songbooks_default_school_idx;
DROP INDEX songbooks_owner_school_idx;
UPDATE songbooks SET owner_kind = 'community' WHERE owner_kind = 'school';
ALTER TABLE songbooks ADD CONSTRAINT songbooks_owner_kind_check CHECK (owner_kind IN ('person', 'band', 'community'));
ALTER TABLE songbooks ADD CONSTRAINT songbooks_check CHECK (
  (owner_kind = 'person'    AND owner_person_id IS NOT NULL AND owner_band_id IS NULL AND owner_community_id IS NULL) OR
  (owner_kind = 'band'      AND owner_band_id IS NOT NULL   AND owner_person_id IS NULL AND owner_community_id IS NULL) OR
  (owner_kind = 'community' AND owner_community_id IS NOT NULL AND owner_person_id IS NULL AND owner_band_id IS NULL)
);
CREATE UNIQUE INDEX songbooks_default_community_idx ON songbooks (owner_community_id) WHERE is_default AND owner_kind = 'community';
CREATE INDEX songbooks_owner_community_idx ON songbooks (owner_community_id) WHERE owner_community_id IS NOT NULL;

ALTER TABLE group_invites DROP CONSTRAINT group_invites_target_kind_check;
ALTER TABLE group_invites DROP CONSTRAINT group_invites_one_target;
UPDATE group_invites SET target_kind = 'community' WHERE target_kind = 'school';
ALTER TABLE group_invites ADD CONSTRAINT group_invites_target_kind_check CHECK (target_kind IN ('band', 'community'));
ALTER TABLE group_invites ADD CONSTRAINT group_invites_one_target CHECK (
  (target_kind = 'band'      AND band_id IS NOT NULL      AND community_id IS NULL) OR
  (target_kind = 'community' AND community_id IS NOT NULL AND band_id      IS NULL)
);

-- Remaining indexes with a "school" name but no literal-value rewrite
-- needed (predicate/body already follows the renamed columns
-- automatically) — renamed for naming consistency only.
ALTER INDEX school_members_school_idx RENAME TO community_members_community_idx;
ALTER INDEX school_members_person_idx RENAME TO community_members_person_idx;
ALTER INDEX school_members_current_unique RENAME TO community_members_current_unique;
ALTER INDEX school_submissions_school_idx RENAME TO community_submissions_community_idx;
ALTER INDEX school_submissions_pending_idx RENAME TO community_submissions_pending_idx;
ALTER INDEX school_submissions_student_idx RENAME TO community_submissions_student_idx;
ALTER INDEX songs_owner_school_idx RENAME TO songs_owner_community_idx;
ALTER INDEX events_school_idx RENAME TO events_community_idx;

-- Primary-key and foreign-key constraint names are NOT updated by
-- ALTER TABLE/COLUMN RENAME (only the index/constraint BODY is kept in
-- sync, never its own name) — rename the ones that still say "school"
-- for consistency. The auto-generated NOT NULL pseudo-constraints
-- (`*_not_null`) are left as-is: pure catalog bookkeeping, never
-- referenced by name anywhere, not worth the churn.
ALTER TABLE communities RENAME CONSTRAINT schools_pkey TO communities_pkey;
ALTER TABLE community_members RENAME CONSTRAINT school_members_pkey TO community_members_pkey;
ALTER TABLE community_members RENAME CONSTRAINT school_members_school_id_fkey TO community_members_community_id_fkey;
ALTER TABLE community_members RENAME CONSTRAINT school_members_person_id_fkey TO community_members_person_id_fkey;
ALTER TABLE community_members RENAME CONSTRAINT school_members_invited_by_person_id_fkey TO community_members_invited_by_person_id_fkey;
ALTER TABLE community_submissions RENAME CONSTRAINT school_submissions_pkey TO community_submissions_pkey;
ALTER TABLE community_submissions RENAME CONSTRAINT school_submissions_school_id_fkey TO community_submissions_community_id_fkey;
ALTER TABLE community_submissions RENAME CONSTRAINT school_submissions_student_person_id_fkey TO community_submissions_student_person_id_fkey;
ALTER TABLE community_submissions RENAME CONSTRAINT school_submissions_source_variant_id_fkey TO community_submissions_source_variant_id_fkey;
ALTER TABLE community_submissions RENAME CONSTRAINT school_submissions_submitted_variant_id_fkey TO community_submissions_submitted_variant_id_fkey;
ALTER TABLE community_submissions RENAME CONSTRAINT school_submissions_decided_by_person_id_fkey TO community_submissions_decided_by_person_id_fkey;
ALTER TABLE songs RENAME CONSTRAINT songs_owner_school_id_fkey TO songs_owner_community_id_fkey;
ALTER TABLE group_invites RENAME CONSTRAINT group_invites_school_id_fkey TO group_invites_community_id_fkey;
ALTER TABLE songbooks RENAME CONSTRAINT songbooks_owner_school_id_fkey TO songbooks_owner_community_id_fkey;
ALTER TABLE setlists RENAME CONSTRAINT setlists_owner_school_id_fkey TO setlists_owner_community_id_fkey;
ALTER TABLE events RENAME CONSTRAINT events_owner_school_id_fkey TO events_owner_community_id_fkey;

-- Serial sequences, same reasoning: not kept in sync by any RENAME TABLE.
ALTER SEQUENCE schools_id_seq RENAME TO communities_id_seq;
ALTER SEQUENCE school_members_id_seq RENAME TO community_members_id_seq;
ALTER SEQUENCE school_submissions_id_seq RENAME TO community_submissions_id_seq;
