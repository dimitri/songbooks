-- Generalize invites beyond "join project X at role Y" to also cover
-- "join band X at status Y" and a plain signup invite with no
-- project/band attachment at all (lets someone register who otherwise
-- has no self-serve path in — there is none today, invites are the only
-- door in). project_id/role become nullable (a band or plain invite has
-- neither); band_id/band_status are the band equivalent of
-- project_id/role, reusing band_members' own status enum rather than
-- inventing a band "role". created_by_person_id records who generated
-- the invite, so accept-time notifications (see store.AcceptInvite) have
-- someone to notify.
ALTER TABLE invites ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE invites ALTER COLUMN role DROP NOT NULL;
ALTER TABLE invites ADD COLUMN band_id bigint REFERENCES bands(id) ON DELETE CASCADE;
ALTER TABLE invites ADD COLUMN band_status band_member_status;
ALTER TABLE invites ADD COLUMN created_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL;

-- An invite targets at most one of {project, band} — never both. (Both
-- null is the plain "just create an account" case.)
ALTER TABLE invites ADD CONSTRAINT invites_one_target
  CHECK (NOT (project_id IS NOT NULL AND band_id IS NOT NULL));
