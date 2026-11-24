-- Band roles, the user's decisions (docs/band-roles.md):
--   * deleting a band is for site admins only: the delete_band privilege
--     goes, from every role;
--   * renaming and archiving the band is a privilege of its own,
--     rename_archive, the core role's by default (and every band's own
--     role that manages roles: its staff); edit_band keeps what the band
--     plays;
--   * a band can be archived (bands.archived_at): read only for
--     everyone — every privilege but rename_archive (to unarchive it)
--     is off while it is — and out of the menus and the home page.

ALTER TABLE bands ADD COLUMN archived_at timestamptz;

DELETE FROM band_role_privileges WHERE privilege = 'delete_band';

INSERT INTO band_role_privileges (role_id, privilege)
  SELECT r.id, 'rename_archive' FROM band_roles r
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
      'propose', 'vote', 'decide_proposals',
      'manage_members', 'remove_members', 'manage_roles',
      'rename_archive', 'edit_band', 'edit_look']) AS p;
  INSERT INTO band_role_privileges (role_id, privilege)
    SELECT guest_id, p FROM unnest(ARRAY[
      'edit_songbook', 'manage_setlists', 'manage_events', 'manage_recordings',
      'propose', 'decide_proposals',
      'remove_members',
      'edit_band']) AS p;
END $$;

-- The one rule, the archive included: an archived band grants nothing
-- but rename_archive (unarchiving it).
CREATE OR REPLACE FUNCTION has_band_privilege(band bigint, person bigint, priv text) RETURNS boolean
LANGUAGE sql STABLE AS $$
  SELECT EXISTS (
    SELECT 1 FROM band_members bm JOIN band_role_privileges brp ON brp.role_id = bm.role_id
    WHERE bm.band_id = band AND bm.person_id = person AND upper_inf(bm.validity) AND brp.privilege = priv)
  AND (priv = 'rename_archive' OR NOT EXISTS (SELECT 1 FROM bands b WHERE b.id = band AND b.archived_at IS NOT NULL))
$$;
