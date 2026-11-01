-- Durable record of every email the dev-mode ConsoleMailer "sent" (see
-- internal/mailer), so magic links can be fetched from a page
-- (GET /dev/outbox, admin-only) instead of tailing server stdout. Not a
-- real send queue/retry mechanism — this instance has no SMTP, by
-- design for this phase.
CREATE TABLE outbox (
  id         bigserial PRIMARY KEY,
  to_email   text NOT NULL,
  subject    text NOT NULL,
  body       text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
