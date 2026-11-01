-- Lightweight in-app notification inbox — not email, not push, just
-- something a logged-in person can check. kind is a free-form tag
-- ("invite_accepted", "band_invite_accepted", "mention", ...) for later
-- filtering; deliberately not an enum, same "free-form string" choice
-- people.instruments made in migration 0018.
CREATE TABLE notifications (
  id         bigserial PRIMARY KEY,
  person_id  bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  kind       text NOT NULL,
  body       text NOT NULL,
  link       text,
  read_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_person_unread_idx ON notifications (person_id, read_at);
