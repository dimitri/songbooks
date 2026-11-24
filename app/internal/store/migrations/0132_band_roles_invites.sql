-- Band roles, round 2 (docs/band-roles.md), the user's decisions:
--   * every way of inviting someone into a band can give any of the
--     band's roles, a band's own included: an invite carries the role
--     (band_role_id) beside its status. An invite made before keeps
--     working with its status (core/guest: the built-in of that name). A
--     deleted role's invites fall back to their status, "guest" for a
--     band's own role (ON DELETE SET NULL);
--   * guests don't edit the band's songs, nor remove members: the
--     built-in guest role of every band loses edit_songbook and
--     remove_members (nothing else of a band's own choices changes).
--     Leaving the band oneself stays every member's (not a privilege);
--   * guests' votes are listed apart, never counted: no privilege
--     changes — a member whose role doesn't grant vote (a guest) may now
--     rate a proposal too, listed apart (store.TallyOf);
--   * removing someone else's Discover and Influences entries is a
--     privilege of its own, curate_listen, the core role's by default
--     (and every band's own role that manages roles: its staff, as
--     migration 0131 did for rename_archive). Whoever added an entry may
--     always remove it.

ALTER TABLE invites ADD COLUMN band_role_id bigint REFERENCES band_roles(id) ON DELETE SET NULL;
ALTER TABLE group_invites ADD COLUMN band_role_id bigint REFERENCES band_roles(id) ON DELETE SET NULL;
ALTER TABLE group_invite_slots ADD COLUMN band_role_id bigint REFERENCES band_roles(id) ON DELETE SET NULL;

DELETE FROM band_role_privileges p USING band_roles r
WHERE p.role_id = r.id AND r.builtin_key = 'guest' AND p.privilege IN ('edit_songbook', 'remove_members');

INSERT INTO band_role_privileges (role_id, privilege)
  SELECT r.id, 'curate_listen' FROM band_roles r
  WHERE r.builtin_key = 'core'
     OR (r.builtin_key IS NULL AND EXISTS (
           SELECT 1 FROM band_role_privileges p WHERE p.role_id = r.id AND p.privilege = 'manage_roles'))
ON CONFLICT DO NOTHING;

-- A new band's roles: kept equal to store.BandRoleDefaults.
CREATE OR REPLACE FUNCTION seed_band_roles(band bigint) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
  core_id bigint;
  guest_id bigint;
BEGIN
  INSERT INTO band_roles (band_id, name, builtin_key) VALUES (band, 'core', 'core') RETURNING id INTO core_id;
  INSERT INTO band_roles (band_id, name, builtin_key) VALUES (band, 'guest', 'guest') RETURNING id INTO guest_id;
  INSERT INTO band_role_privileges (role_id, privilege)
    SELECT core_id, p FROM unnest(ARRAY[
      'edit_songbook', 'publish', 'manage_setlists', 'manage_events', 'manage_recordings',
      'propose', 'vote', 'decide_proposals', 'curate_listen',
      'manage_members', 'remove_members', 'manage_roles',
      'rename_archive', 'edit_band', 'edit_look']) AS p;
  INSERT INTO band_role_privileges (role_id, privilege)
    SELECT guest_id, p FROM unnest(ARRAY[
      'manage_setlists', 'manage_events', 'manage_recordings',
      'propose', 'decide_proposals',
      'edit_band']) AS p;
END $$;
