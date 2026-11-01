-- Initial schema: projects/people/songs/setlists plus the jam-mode and
-- auth tables used by later phases. See the "Songbooks Platform Strategy"
-- memo (rev 4) for the design rationale — Postgres as the system of
-- record, trigger-populated song_revisions instead of a git commit per
-- save, RLS for jam-mode read access (policies land in a later migration
-- once jam mode's app-side code exists).

CREATE TABLE people (
  id            bigserial PRIMARY KEY,
  username      text UNIQUE NOT NULL,
  display_name  text NOT NULL,
  password_hash text,
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE projects (
  id           bigserial PRIMARY KEY,
  slug         text UNIQUE NOT NULL,
  name         text NOT NULL,
  playlist_url text NOT NULL DEFAULT '',
  song_order   jsonb NOT NULL DEFAULT '[]',
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE project_role AS ENUM ('owner', 'editor', 'viewer');

CREATE TABLE project_members (
  project_id bigint NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  person_id  bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  role       project_role NOT NULL,
  PRIMARY KEY (project_id, person_id)
);

CREATE TABLE invites (
  token      text PRIMARY KEY,
  project_id bigint NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  role       project_role NOT NULL,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE songs (
  id            bigserial PRIMARY KEY,
  project_id    bigint NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  stem          text NOT NULL,
  title         text NOT NULL DEFAULT '',
  artist        text NOT NULL DEFAULT '',
  reference_url text NOT NULL DEFAULT '',
  body          text NOT NULL,
  search        tsvector GENERATED ALWAYS AS (
                   to_tsvector('english', coalesce(title, '') || ' ' || coalesce(artist, '') || ' ' || body)
                 ) STORED,
  version       integer NOT NULL DEFAULT 1,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, stem)
);
CREATE INDEX songs_search_idx ON songs USING GIN (search);
CREATE INDEX songs_project_idx ON songs (project_id);

CREATE TABLE song_revisions (
  id        bigserial PRIMARY KEY,
  song_id   bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  body      text NOT NULL,
  edited_by bigint REFERENCES people(id),
  edited_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION songs_archive_revision() RETURNS trigger AS $$
BEGIN
  INSERT INTO song_revisions (song_id, body, edited_at) VALUES (OLD.id, OLD.body, now());
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER songs_before_update
  BEFORE UPDATE ON songs
  FOR EACH ROW
  WHEN (OLD.body IS DISTINCT FROM NEW.body)
  EXECUTE FUNCTION songs_archive_revision();

CREATE TABLE setlists (
  id         bigserial PRIMARY KEY,
  project_id bigint NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  stem       text NOT NULL,
  title      text NOT NULL DEFAULT '',
  body       text NOT NULL,
  version    integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (project_id, stem)
);

CREATE TABLE jam_events (
  id         bigserial PRIMARY KEY,
  project_id bigint NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  code       text UNIQUE NOT NULL,
  name       text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL
);

CREATE TABLE jam_profiles (
  id           bigserial PRIMARY KEY,
  jam_event_id bigint NOT NULL REFERENCES jam_events(id) ON DELETE CASCADE,
  nickname     text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE jam_picks (
  jam_event_id   bigint NOT NULL REFERENCES jam_events(id) ON DELETE CASCADE,
  jam_profile_id bigint NOT NULL REFERENCES jam_profiles(id) ON DELETE CASCADE,
  song_id        bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  instrument     text NOT NULL,
  PRIMARY KEY (jam_event_id, jam_profile_id, song_id, instrument)
);
