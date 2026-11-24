-- A band's Settings in sections (docs/band-settings-proposal.md): its ⚙
-- shows to whoever may change one of them. Saying what the band plays
-- (edit_band) was the guest role's by default, and the only reason every
-- guest got the ⚙: it is a set-once choice, now core's only by default.
--
--   * every band's built-in guest role that still grants exactly what
--     migration 0132 gave it loses edit_band; a band that changed what its
--     guests may do keeps its choice, edit_band included;
--   * a new band's guest role starts without it (seed_band_roles, kept
--     equal to store.BandRoleDefaults);
--   * edit_band now also covers the language of the band's pages and its
--     tagline and website (the routes' checks, not the data): they sit
--     with what the band plays in the band's Settings, The band section;
--     edit_look is the Look section alone.

DELETE FROM band_role_privileges p USING band_roles r
WHERE p.role_id = r.id AND r.builtin_key = 'guest' AND p.privilege = 'edit_band'
  AND (SELECT array_agg(q.privilege ORDER BY q.privilege) FROM band_role_privileges q WHERE q.role_id = r.id)
      = ARRAY['decide_proposals', 'edit_band', 'manage_events', 'manage_recordings', 'manage_setlists', 'propose'];

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
      'propose', 'decide_proposals']) AS p;
END $$;
