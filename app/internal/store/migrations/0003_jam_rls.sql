-- Jam mode's row-level security: an unlogged, nickname-only jam_profiles
-- participant gets scoped read access to exactly one event's project songs,
-- enforced by Postgres itself rather than hand-rolled Go filtering.
--
-- Design note (deviates from the strategy memo's literal draft SQL): the
-- memo's sketch assumed the app's normal connection would set
-- app.person_id/app.jam_code per request and run under
-- FORCE ROW LEVEL SECURITY. The app's existing query code (Phases 1-3, see
-- store.go) has no per-request transaction/session-variable plumbing at
-- all -- every method runs a bare pool Exec/Query. Forcing RLS on the table
-- the app's own (owning) role connects as would have broken every existing
-- read/write path at once, since none of them set app.person_id.
--
-- Instead: RLS is enabled but NOT forced. A table owner always bypasses
-- RLS unless forced, so every existing Phase 1-3 code path (which connects
-- as the owning role) is completely unaffected -- zero query changes
-- anywhere outside this migration and jam.go's one new read path. Only a
-- genuinely untrusted/limited path -- jam-mode's song listing -- runs
-- through a dedicated, non-owning role, which is unconditionally subject
-- to RLS with no FORCE needed. That is the standard Postgres pattern for
-- "give this one narrow, lower-trust code path a structural guarantee,"
-- and it is actually a better fit here than fighting the existing
-- architecture to force RLS everywhere for a memo-level sketch.
ALTER TABLE songs ENABLE ROW LEVEL SECURITY;

CREATE OR REPLACE FUNCTION current_jam_project() RETURNS bigint AS $$
  SELECT je.project_id FROM jam_events je
  WHERE je.code = nullif(current_setting('app.jam_code', true), '')
    AND je.expires_at > now()
$$ LANGUAGE sql STABLE;

DROP POLICY IF EXISTS jam_read ON songs;
CREATE POLICY jam_read ON songs FOR SELECT
  USING (project_id = current_jam_project());

-- Dedicated low-privilege role for the jam-mode read path only. It is NOT
-- the table owner, so the jam_read policy above applies to it
-- unconditionally. The password below is a fixed local-dev placeholder --
-- flagged here, not silently shipped: a real deployment needs a generated
-- secret and a proper secrets story before this goes anywhere near the
-- Scaleway box.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'songbooks_jam') THEN
    CREATE ROLE songbooks_jam LOGIN PASSWORD 'songbooks_jam_dev_only_change_me';
  END IF;
END
$$;

DO $$
BEGIN
  EXECUTE format('GRANT CONNECT ON DATABASE %I TO songbooks_jam', current_database());
END
$$;
GRANT USAGE ON SCHEMA public TO songbooks_jam;
-- Needed so the jam_read policy's own current_jam_project() function (run
-- with the CALLER's privileges, i.e. songbooks_jam's, since it is not
-- SECURITY DEFINER) can read jam_events at all.
GRANT SELECT ON jam_events TO songbooks_jam;
GRANT SELECT (id, project_id, stem, title, artist, reference_url, body, version, created_at) ON songs TO songbooks_jam;
