-- Single-use, short-lived magic-link login tokens (GET /login/confirm).
-- Separate from sessions (0002_sessions.sql): a login_tokens row proves
-- "this email address requested a link and clicked it once", a
-- sessions row is the resulting logged-in browser session.
CREATE TABLE login_tokens (
  id         bigserial PRIMARY KEY,
  token      text UNIQUE NOT NULL,
  person_id  bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_tokens_person_idx ON login_tokens (person_id);
