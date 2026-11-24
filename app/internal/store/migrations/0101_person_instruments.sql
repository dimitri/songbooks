-- Instruments in the profile: what each person plays, one row per
-- canonical instrument (internal/instruments), with a self-assessed
-- level and where it came from.
--
-- people.instruments (0018) stays, kept in step by the store as the
-- plain list of what's here (canonical IDs, or the person's own word for
-- an "other"), so the pages that read it (band rosters, member lists)
-- keep working. The free text it held is kept per row in original.
--
-- The rows are filled from people.instruments, band roster slots and
-- event program singers by store.BackfillPersonInstruments (Go, because
-- reading free text needs instruments.Normalize), run after migrating;
-- it only adds what's missing, so running it again changes nothing.
CREATE TABLE person_instruments (
  id                  bigserial PRIMARY KEY,
  person_id           bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  instrument          text NOT NULL,
  -- the person's own word for an "other" instrument ("theremin"); ''
  -- for the canonical ones
  other_name          text NOT NULL DEFAULT '',
  -- self-assessed; NULL until the person says
  level               text CHECK (level IN ('beginner', 'intermediate', 'advanced', 'expert')),
  -- where it was first collected from: the person themselves, a band
  -- (its roster), or an event (registering to play, singing in its
  -- program); the band/event, when it still exists
  source              text NOT NULL DEFAULT 'self' CHECK (source IN ('self', 'band', 'community', 'event')),
  source_band_id      bigint REFERENCES bands(id) ON DELETE SET NULL,
  source_community_id bigint REFERENCES communities(id) ON DELETE SET NULL,
  source_event_id     bigint REFERENCES events(id) ON DELETE SET NULL,
  -- the free text it was read from ("guitar & singer"), for display and audit
  original            text NOT NULL DEFAULT '',
  -- taken off by the person: hidden, and not collected again
  removed_at          timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  CHECK (instrument = 'other' OR other_name = ''),
  CHECK (instrument <> 'other' OR other_name <> '')
);
CREATE UNIQUE INDEX person_instruments_unique ON person_instruments (person_id, instrument, lower(other_name));

-- Who may see a person's levels besides themselves and the app's admins:
-- with this on (the default), their bandmates and the organisers of
-- their communities and of the events they register for; off, nobody.
ALTER TABLE people ADD COLUMN share_instrument_levels boolean NOT NULL DEFAULT true;

-- What someone registering for a jam (or any event where people play)
-- says they'll play: canonical instrument IDs.
ALTER TABLE event_registrations ADD COLUMN instruments text[] NOT NULL DEFAULT '{}';
