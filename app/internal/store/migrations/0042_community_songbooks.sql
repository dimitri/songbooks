-- Generalizes "school" into the user-facing "Community" concept: a named
-- group with read-write staff (the existing 'instructor' role) and
-- read-only members (the existing 'student' role), full access to the
-- community's own songbooks and setlists. Keeps the underlying
-- schools/school_members tables and Go identifiers as-is (zero rows in
-- either today — confirmed before writing this — so there is no real
-- data to migrate, and a full rename would touch a large surface for no
-- functional benefit); only the user-facing copy changes to
-- Community/Staff/Member, done at the template layer, not here.

-- A school/community can now own multiple named songbooks, same as a
-- band or a person already can, instead of the separate single-list
-- school_catalog_entries mechanism.
ALTER TABLE songbooks ADD COLUMN owner_school_id bigint REFERENCES schools(id) ON DELETE CASCADE;

-- school_catalog_entries is empty in production (confirmed) and is now
-- fully superseded by songbook_entries against a school-owned songbook.
DROP TABLE school_catalog_entries;

-- Setlists: a community can own its own setlists directly, same as a
-- band. 'school' joins 'person'/'band' as a valid scope ('project' stays
-- the dead, un-droppable enum leftover from migration 0041).
ALTER TYPE setlist_scope ADD VALUE 'school';
