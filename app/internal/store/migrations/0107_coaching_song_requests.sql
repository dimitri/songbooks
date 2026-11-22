-- A student's "Ask for it" (docs/coaching.md, "The songbooks"): a song of
-- the coaching community's repertoire a student would like in their own
-- songbook, waiting on the coach, who adds it (in the student's key) or
-- lets it go. One row per student and song: asking again after a "not
-- now" makes it pending again. (0103-0106 are other branches'.)
CREATE TABLE coaching_song_requests (
  id                   bigserial PRIMARY KEY,
  community_id         bigint NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
  person_id            bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  variant_id           bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  status               text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'added', 'declined')),
  created_at           timestamptz NOT NULL DEFAULT now(),
  decided_at           timestamptz,
  decided_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  UNIQUE (community_id, person_id, variant_id)
);
CREATE INDEX coaching_song_requests_pending ON coaching_song_requests (community_id, created_at) WHERE status = 'pending';
