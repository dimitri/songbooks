-- School mode: a school is a named group with a REAL two-tier role
-- (instructor/student), unlike a band (deliberately flat, no hierarchy —
-- see migration 0010's comment). Structurally a close cousin of bands
-- (same history-preserving membership table shape, same pointer-not-copy
-- catalog table), but with instructor/student instead of core/guest and
-- an asymmetric permission model: instructors manage membership/catalog
-- and review student submissions; students get read-only access to the
-- school's repertoire plus their own personal space (already works,
-- nothing school-specific needed for that part — see the brief).
CREATE TABLE schools (
  id         bigserial PRIMARY KEY,
  name       text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE school_member_role AS ENUM ('instructor', 'student');

-- Same history-preserving shape as band_members (migration 0010):
-- every row ever inserted is kept forever, current membership = left_at
-- IS NULL, re-joining inserts a NEW row rather than reusing the old one.
CREATE TABLE school_members (
  id                   bigserial PRIMARY KEY,
  school_id            bigint NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
  person_id            bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  role                 school_member_role NOT NULL,
  joined_at            timestamptz NOT NULL DEFAULT now(),
  left_at              timestamptz,
  invited_by_person_id bigint REFERENCES people(id)
);
CREATE INDEX school_members_school_idx ON school_members (school_id, left_at);
CREATE INDEX school_members_person_idx ON school_members (person_id, left_at);
-- At most one CURRENT row per (school, person) — same uniqueness rule as
-- band_members_current_unique.
CREATE UNIQUE INDEX school_members_current_unique ON school_members (school_id, person_id) WHERE left_at IS NULL;

-- School catalog: "this variant is in school X's repertoire" — same
-- pointer mechanism as band_catalog_entries (migration 0012), not a copy.
CREATE TABLE school_catalog_entries (
  id                 bigserial PRIMARY KEY,
  school_id          bigint NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
  variant_id         bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  added_at           timestamptz NOT NULL DEFAULT now(),
  added_by_person_id bigint REFERENCES people(id),
  UNIQUE (school_id, variant_id)
);

CREATE TYPE school_submission_kind AS ENUM ('new_song', 'edit');
CREATE TYPE school_submission_status AS ENUM ('pending', 'accepted', 'rejected');

-- A student's submission of work for the school's consideration — kept
-- as its own table rather than generalizing band_proposals across two
-- entity types: this is instructor-reviewed (institutional accept/
-- reject), not peer-voted, and band_proposals carries veto-voting
-- columns that don't apply here. See internal/store/school_submissions.go
-- for the accept/reject logic and the revision-attribution judgment call.
--
-- submitted_variant_id is always the STUDENT's own personal-scope
-- variant (their fork/draft, created via the normal phase-1
-- CreateSong/fork flow before ever submitting). source_variant_id is
-- set only for kind='edit': the school/global variant being edited: on
-- accept, the submitted variant's content is applied onto it.
CREATE TABLE school_submissions (
  id                    bigserial PRIMARY KEY,
  school_id             bigint NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
  student_person_id     bigint NOT NULL REFERENCES people(id),
  kind                  school_submission_kind NOT NULL,
  source_variant_id     bigint REFERENCES songs(id) ON DELETE SET NULL,
  submitted_variant_id  bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  note                  text NOT NULL DEFAULT '',
  status                school_submission_status NOT NULL DEFAULT 'pending',
  created_at            timestamptz NOT NULL DEFAULT now(),
  decided_at            timestamptz,
  decided_by_person_id  bigint REFERENCES people(id)
);
CREATE INDEX school_submissions_school_idx ON school_submissions (school_id, created_at DESC);
CREATE INDEX school_submissions_pending_idx ON school_submissions (school_id, status);
CREATE INDEX school_submissions_student_idx ON school_submissions (student_person_id, created_at DESC);
