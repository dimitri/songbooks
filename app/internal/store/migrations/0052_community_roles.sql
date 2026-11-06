-- Community privilege/role system: replaces the fixed staff/member
-- two-tier enum with per-community, configurable roles (a name plus a
-- set of privileges from a fixed code-defined catalog — see
-- internal/store/roles.go). Every community gets three built-in roles,
-- seeded here for existing communities and by store.CreateCommunity for
-- new ones:
--   staff  — every privilege (replaces the old 'staff' enum value)
--   member — edit_repertoire only (replaces the old 'member' enum value)
--   guest  — no privileges (read-only); new — the role an invite link or
--            QR code can now target for open/anonymous-feeling signup,
--            e.g. a jam-type community event, without the old no-login
--            jam_profiles system this replaces (see migration 0051).
-- A community's staff can rename/delete a built-in role's PRIVILEGES
-- (not its builtin_key) or create further custom roles entirely.

CREATE TABLE community_roles (
  id           bigserial PRIMARY KEY,
  community_id bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  name         text NOT NULL,
  builtin_key  text, -- 'staff'/'member'/'guest' for a built-in role, NULL for a custom one
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (community_id, name)
);
CREATE INDEX community_roles_community_idx ON community_roles (community_id);

-- The privilege catalog itself is fixed and code-defined, not a DB
-- table — only which privileges a given role GRANTS is per-role data.
CREATE TABLE community_role_privileges (
  role_id   bigint NOT NULL REFERENCES community_roles(id) ON DELETE CASCADE,
  privilege text NOT NULL,
  PRIMARY KEY (role_id, privilege)
);

ALTER TABLE community_members ADD COLUMN role_id bigint REFERENCES community_roles(id);

-- Seed the 3 built-in roles (+ privileges) for every existing community,
-- then point every existing membership row at the one matching its old
-- role enum value.
DO $$
DECLARE
  c RECORD;
  staff_id bigint;
  member_id bigint;
  guest_id bigint;
BEGIN
  FOR c IN SELECT id FROM communities LOOP
    INSERT INTO community_roles (community_id, name, builtin_key) VALUES (c.id, 'staff', 'staff') RETURNING id INTO staff_id;
    INSERT INTO community_roles (community_id, name, builtin_key) VALUES (c.id, 'member', 'member') RETURNING id INTO member_id;
    INSERT INTO community_roles (community_id, name, builtin_key) VALUES (c.id, 'guest', 'guest') RETURNING id INTO guest_id;

    INSERT INTO community_role_privileges (role_id, privilege)
      SELECT staff_id, p FROM unnest(ARRAY['edit_repertoire', 'manage_members', 'manage_events', 'manage_submissions', 'manage_settings']) AS p;
    INSERT INTO community_role_privileges (role_id, privilege) VALUES (member_id, 'edit_repertoire');
    -- guest: no rows at all — no privileges, read-only.

    UPDATE community_members SET role_id = staff_id WHERE community_id = c.id AND role = 'staff';
    UPDATE community_members SET role_id = member_id WHERE community_id = c.id AND role = 'member';
  END LOOP;
END $$;

ALTER TABLE community_members ALTER COLUMN role_id SET NOT NULL;
ALTER TABLE community_members DROP COLUMN role;

-- communities.default_join_role held a 'staff'/'member' enum value
-- naming the role a new joiner gets through the community's own
-- open-join default; group_invites.default_status held the same idea
-- per-invite-link. Both become plain role-NAME text (resolved against
-- that community's own community_roles at join/claim time), so a
-- custom role — including the new 'guest' built-in, e.g. for a jam
-- event's QR code — can be targeted too, not just the old two values.
ALTER TABLE communities ALTER COLUMN default_join_role TYPE text USING default_join_role::text;
ALTER TABLE communities ALTER COLUMN default_join_role SET DEFAULT 'member';

DROP TYPE community_member_role;
