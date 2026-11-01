-- Session tokens for password-auth logins (Phase 2: identity/roles/invites).
CREATE TABLE sessions (
  token      text PRIMARY KEY,
  person_id  bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_person_idx ON sessions (person_id);
