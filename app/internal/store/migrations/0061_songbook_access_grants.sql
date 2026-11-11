-- Per-songbook access grants: a community can have a songbook that isn't
-- open to every current member — the paywall case (Florent's situation:
-- members buy access to one course/songbook at a time, not the whole
-- community). requires_grant marks a songbook as gated; when true,
-- CanAccessSongbook/CanViewVariant need an explicit row here in addition
-- to ordinary community membership. Billing/payment itself is a later
-- phase — this only adds the grant primitive so that later phase has
-- something to write to.
ALTER TABLE songbooks ADD COLUMN requires_grant boolean NOT NULL DEFAULT false;

CREATE TABLE songbook_access_grants (
  id                   bigserial PRIMARY KEY,
  songbook_id          bigint NOT NULL REFERENCES songbooks(id) ON DELETE CASCADE,
  person_id            bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  granted_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  granted_at           timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX songbook_access_grants_unique ON songbook_access_grants (songbook_id, person_id);
CREATE INDEX songbook_access_grants_person_idx ON songbook_access_grants (person_id);
