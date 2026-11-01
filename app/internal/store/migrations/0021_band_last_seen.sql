-- Phase 7: "changes since your last visit" to a band page. Tracked
-- separately from band_members rather than as a column on it, because
-- band_members is history-preserving (a fresh row per join/leave/rejoin,
-- see migration 0010) and last_seen_at needs to update on every page view
-- without ever touching — or being confused with — that membership
-- history. One row per (band, person), upserted in place every time.
CREATE TABLE band_member_last_seen (
  band_id   bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  person_id bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  seen_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (band_id, person_id)
);
