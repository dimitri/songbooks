-- Retires the old anonymous, no-login jam system entirely. It predates
-- the community model (migration 0023+) and never overlapped with it: a
-- jam_events row was scoped to a songbook, joined by a nickname-only
-- jam_profiles identity with no account at all. Jams now only exist as
-- a community event (event_type='jam', see migration 0049) — a real
-- account registers, same as for any other event type. Confirmed zero
-- rows in production across every jam_* table before writing this
-- migration (the staging area was never used for a real jam), so this
-- is a pure schema retirement, not a data migration.

-- section_annotations: drop its jam-participant columns/constraint,
-- keep the table/feature itself for logged-in-person annotations (its
-- only remaining use).
ALTER TABLE section_annotations DROP CONSTRAINT section_annotations_annotator_check;
ALTER TABLE section_annotations DROP COLUMN jam_event_id;
ALTER TABLE section_annotations DROP COLUMN jam_profile_id;
ALTER TABLE section_annotations ALTER COLUMN person_id SET NOT NULL;

-- songs: drop the jam-scoped ownership column/index. The 'jam' label on
-- variant_visibility is left in place but permanently unused — Postgres
-- can't cheaply drop a single enum label (it would mean rebuilding the
-- type and every dependent column for a label nothing will ever write
-- again), and an unused label is harmless.
DROP INDEX songs_owner_jam_event_idx;
ALTER TABLE songs DROP COLUMN owner_jam_event_id;

-- The jam tables themselves, leaf-first. jam_events.called_profile_id
-- (migration 0007) references jam_profiles, which in turn references
-- jam_events via jam_event_id — a circular FK between the two — so that
-- column is dropped first to break the cycle before either table goes.
ALTER TABLE jam_events DROP COLUMN called_profile_id;
DROP TABLE jam_song_instrument_limits;
DROP TABLE jam_picks;
DROP TABLE jam_songs;
DROP TABLE jam_organizers;
DROP TABLE jam_profiles;
DROP TABLE jam_events;
