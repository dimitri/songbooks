-- Band roles (docs/band-roles.md): a band's members hold a role, a name
-- plus a set of privileges from a fixed, code-defined catalog (the
-- BandPriv* constants in internal/store/band_roles.go), the same model a
-- community's roles use (migration 0052, internal/store/roles.go).
--
-- Every band has two built-in roles, "core" and "guest", holding
-- exactly the rights core and guest members had before this migration;
-- a band can change what they grant, and add roles of its own (e.g. a
-- "Staff" who alone decides the proposals). band_members.status stays,
-- kept in step with the role by a trigger: core for the "core" role and
-- for every custom role, guest for the "guest" role; setting status
-- (older code, invites, a revert) points the row at that built-in role.
-- Nothing is lost: every membership row, past ones included, gets the
-- built-in role matching its status.

CREATE TABLE band_roles (
  id          bigserial PRIMARY KEY,
  band_id     bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  name        text NOT NULL,
  builtin_key text CHECK (builtin_key IN ('core', 'guest')), -- NULL: a band's own role
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (band_id, name)
);
CREATE UNIQUE INDEX band_roles_builtin_idx ON band_roles (band_id, builtin_key) WHERE builtin_key IS NOT NULL;

CREATE TABLE band_role_privileges (
  role_id   bigint NOT NULL REFERENCES band_roles(id) ON DELETE CASCADE,
  privilege text NOT NULL,
  PRIMARY KEY (role_id, privilege)
);

-- The built-in roles' privileges: today's rights (the inventory in
-- docs/band-roles.md). Kept equal to store.BandRoleDefaults (a test
-- checks it).
CREATE FUNCTION seed_band_roles(band bigint) RETURNS void LANGUAGE plpgsql AS $$
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
      'edit_band', 'edit_look', 'delete_band']) AS p;
  INSERT INTO band_role_privileges (role_id, privilege)
    SELECT guest_id, p FROM unnest(ARRAY[
      'edit_songbook', 'manage_setlists', 'manage_events', 'manage_recordings',
      'propose', 'decide_proposals',
      'remove_members',
      'edit_band', 'delete_band']) AS p;
END $$;

SELECT seed_band_roles(id) FROM bands;

-- A new band gets its two roles as it's made, whatever makes it.
CREATE FUNCTION bands_seed_roles() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  PERFORM seed_band_roles(NEW.id);
  RETURN NULL;
END $$;
CREATE TRIGGER bands_seed_roles AFTER INSERT ON bands FOR EACH ROW EXECUTE FUNCTION bands_seed_roles();

ALTER TABLE band_members ADD COLUMN role_id bigint REFERENCES band_roles(id);
UPDATE band_members m SET role_id = r.id FROM band_roles r WHERE r.band_id = m.band_id AND r.builtin_key = m.status::text;
ALTER TABLE band_members ALTER COLUMN role_id SET NOT NULL;
CREATE INDEX band_members_role_idx ON band_members (role_id);

-- status and role_id kept in step. A row written with a status only (an
-- invite, the core/guest switch, a revert) gets that built-in role; a
-- row given a role gets its status (guest for the guest role, core for
-- any other). A role is its own band's only.
CREATE FUNCTION band_members_role_sync() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  role_band bigint;
  role_key text;
BEGIN
  IF (TG_OP = 'INSERT' AND NEW.role_id IS NULL)
     OR (TG_OP = 'UPDATE' AND NEW.role_id IS NOT DISTINCT FROM OLD.role_id AND NEW.status IS DISTINCT FROM OLD.status) THEN
    SELECT id INTO NEW.role_id FROM band_roles WHERE band_id = NEW.band_id AND builtin_key = NEW.status::text;
    RETURN NEW;
  END IF;
  SELECT band_id, builtin_key INTO role_band, role_key FROM band_roles WHERE id = NEW.role_id;
  IF role_band IS DISTINCT FROM NEW.band_id THEN
    RAISE EXCEPTION 'role % is not a role of band %', NEW.role_id, NEW.band_id USING ERRCODE = 'foreign_key_violation';
  END IF;
  NEW.status := (CASE WHEN role_key = 'guest' THEN 'guest' ELSE 'core' END)::band_member_status;
  RETURN NEW;
END $$;
CREATE TRIGGER band_members_role_sync BEFORE INSERT OR UPDATE OF status, role_id ON band_members
  FOR EACH ROW EXECUTE FUNCTION band_members_role_sync();

-- Whether person's current role in band grants priv: the one rule
-- (store.HasBandPrivilege), for queries that need it inline.
CREATE FUNCTION has_band_privilege(band bigint, person bigint, priv text) RETURNS boolean
LANGUAGE sql STABLE AS $$
  SELECT EXISTS (
    SELECT 1 FROM band_members bm JOIN band_role_privileges brp ON brp.role_id = bm.role_id
    WHERE bm.band_id = band AND bm.person_id = person AND upper_inf(bm.validity) AND brp.privilege = priv)
$$;
