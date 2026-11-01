-- Reverses 0003_jam_rls.sql. Decision (user, this session): "Drop RLS we
-- don't need security, we just need to avoid confusion." The dedicated
-- songbooks_jam role + RLS policy added a second Postgres role, a second
-- pgxpool, and per-transaction session GUCs for exactly one read path
-- (ListJamSongs) that a plain `WHERE project_id = $1` already does just as
-- correctly, with far less to reason about. Jam-mode's songs read is no
-- more "untrusted" than any other per-project read in this app (a jam code
-- is itself the access control, same trust level as a project slug) — RLS
-- here was solving a problem this app doesn't actually have.
DROP POLICY IF EXISTS jam_read ON songs;
ALTER TABLE songs DISABLE ROW LEVEL SECURITY;
DROP FUNCTION IF EXISTS current_jam_project();

-- Revoke everything granted to songbooks_jam in 0003, then drop the role.
-- Wrapped in a DO block so a leftover dependency (e.g. a default-privilege
-- entry nothing above anticipated) logs a clear notice and leaves the role
-- in place rather than failing this whole migration — see store.go/jam.go,
-- which no longer reference this role at all either way.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'songbooks_jam') THEN
    REVOKE ALL PRIVILEGES ON songs FROM songbooks_jam;
    REVOKE ALL PRIVILEGES ON jam_events FROM songbooks_jam;
    REVOKE USAGE ON SCHEMA public FROM songbooks_jam;
    EXECUTE format('REVOKE CONNECT ON DATABASE %I FROM songbooks_jam', current_database());
    BEGIN
      DROP ROLE songbooks_jam;
    EXCEPTION WHEN dependent_objects_still_exist OR OTHERS THEN
      RAISE NOTICE 'could not drop role songbooks_jam (leftover dependency) -- revoked its grants and left it in place, unused: %', SQLERRM;
    END;
  END IF;
END
$$;
