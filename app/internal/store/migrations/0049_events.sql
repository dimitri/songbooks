-- Events: a community hosts public events (jam/scène ouverte, meetup,
-- listening party, dinner, "going to a show together") that anyone can
-- register for; a band hosts its own events (concerts/gigs) scoped to
-- its own members only, never public. Both share one table/shape — an
-- event always has a name, a date, a place, and (per the user's own
-- spec) an add-to-calendar button — but only a school-owned event gets
-- a public listing/registration page; a band-owned event's visibility
-- follows the band's own membership gate (requireBandMember).
--
-- audience_scope only matters for jam-type events: 'members' (musicians
-- sign up individually, today's jam_profiles shape), 'bands' (a whole
-- band claims a slot — the French "scène ouverte"), or 'both'.
CREATE TYPE event_owner_kind AS ENUM ('school', 'band');
CREATE TYPE event_type AS ENUM ('jam', 'concert', 'meetup', 'listening_party', 'dinner', 'show_together');
CREATE TYPE event_audience_scope AS ENUM ('members', 'bands', 'both');

CREATE TABLE events (
  id                    bigserial PRIMARY KEY,
  owner_kind            event_owner_kind NOT NULL,
  owner_school_id       bigint REFERENCES schools(id) ON DELETE CASCADE,
  owner_band_id         bigint REFERENCES bands(id) ON DELETE CASCADE,
  event_type            event_type NOT NULL,
  name                  text NOT NULL,
  description           text NOT NULL DEFAULT '',
  location              text NOT NULL DEFAULT '',
  starts_at             timestamptz NOT NULL,
  ends_at               timestamptz,
  audience_scope        event_audience_scope NOT NULL DEFAULT 'members',
  -- The event's own setlist — what an assigned event manager edits, and
  -- what song registrations (below) ultimately point songs into. NULL
  -- until a manager creates/attaches one; an event can exist (and be
  -- listed, registered for) before its setlist is built out.
  setlist_id            bigint REFERENCES setlists(id) ON DELETE SET NULL,
  created_by_person_id  bigint REFERENCES people(id) ON DELETE SET NULL,
  created_at            timestamptz NOT NULL DEFAULT now(),
  CHECK (
    (owner_kind = 'school' AND owner_school_id IS NOT NULL AND owner_band_id IS NULL) OR
    (owner_kind = 'band'   AND owner_band_id IS NOT NULL   AND owner_school_id IS NULL)
  )
);
CREATE INDEX events_school_idx ON events (owner_school_id, starts_at) WHERE owner_school_id IS NOT NULL;
CREATE INDEX events_band_idx ON events (owner_band_id, starts_at) WHERE owner_band_id IS NOT NULL;
CREATE INDEX events_starts_idx ON events (starts_at);

-- Event managers: per-event assignment (not "all community staff
-- automatically manage every event" — the user's own explicit choice),
-- made by community/band staff. An event manager's whole job is "manage
-- the songbook and the setlist" for that one event — see requireEventManager
-- in httpapi for what that gates.
CREATE TABLE event_managers (
  id                     bigserial PRIMARY KEY,
  event_id               bigint NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  person_id              bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  assigned_by_person_id  bigint REFERENCES people(id) ON DELETE SET NULL,
  assigned_at            timestamptz NOT NULL DEFAULT now(),
  UNIQUE (event_id, person_id)
);

-- "Register for this event" — a real account, created on the spot if
-- the registrant doesn't have one yet (same live-account-creation
-- convention as a plain/group invite's accept step, not an anonymous
-- jam-style nickname: the user's own explicit choice). band_id is set
-- when registering AS a band (the whole band is attending/playing),
-- NULL for an individual registrant.
CREATE TABLE event_registrations (
  id             bigserial PRIMARY KEY,
  event_id       bigint NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  person_id      bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  band_id        bigint REFERENCES bands(id) ON DELETE SET NULL,
  registered_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (event_id, person_id)
);

-- "Claims a setlist slot" — registering to actually PLAY a specific
-- song at the event, as a band or as an individual musician depending
-- on how the event's audience_scope is set up. Keyed by (event, song),
-- not by a positional slot index: setlist entries live as text lines
-- inside setlists.body (see internal/setlist), with no durable per-
-- entry row id to reference, so the song itself is the stable key a
-- registration can point at without breaking if the setlist is
-- reordered.
CREATE TABLE event_song_registrations (
  id             bigserial PRIMARY KEY,
  event_id       bigint NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  variant_id     bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  band_id        bigint REFERENCES bands(id) ON DELETE CASCADE,
  person_id      bigint REFERENCES people(id) ON DELETE CASCADE,
  registered_at  timestamptz NOT NULL DEFAULT now(),
  CHECK ((band_id IS NOT NULL AND person_id IS NULL) OR (band_id IS NULL AND person_id IS NOT NULL))
);
CREATE INDEX event_song_registrations_event_idx ON event_song_registrations (event_id);

-- "Apply community edited defaults": the role a person is granted when
-- an event registration joins them to the community automatically
-- (see RegisterForEvent) — editable by community staff, same spirit as
-- every other per-community setting on this table.
ALTER TABLE schools ADD COLUMN default_join_role school_member_role NOT NULL DEFAULT 'member';
