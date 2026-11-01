-- Jam organizer: a person who can control song order and "who's up next"
-- for one specific jam event, distinct from project-wide editor/owner
-- (an organizer for a jam need not have project-wide editor rights, and
-- an editor/owner always qualifies too — see httpapi.requireJamOrganizer).
CREATE TABLE jam_organizers (
  jam_event_id bigint NOT NULL REFERENCES jam_events(id) ON DELETE CASCADE,
  person_id    bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  PRIMARY KEY (jam_event_id, person_id)
);

-- "Currently called" state: who's up next, on what instrument, for which
-- song — small enough to live directly on jam_events rather than a side
-- table. NULL columns mean "nobody currently called."
ALTER TABLE jam_events ADD COLUMN called_profile_id bigint REFERENCES jam_profiles(id) ON DELETE SET NULL;
ALTER TABLE jam_events ADD COLUMN called_song_id    bigint REFERENCES songs(id) ON DELETE SET NULL;
ALTER TABLE jam_events ADD COLUMN called_instrument text;
ALTER TABLE jam_events ADD COLUMN called_at         timestamptz;
