-- A band's recordings (docs/band-recordings.md): the files of a folder
-- the band shares on pCloud, indexed — never stored. A member gives the
-- folder's public link once; a sync lists it, and each recording gets what
-- its folder and file name say: a rehearsal, a concert, an audition...,
-- its date, its place in the session, the song, the take.
CREATE TABLE band_recording_sources (
  id                   bigserial PRIMARY KEY,
  band_id              bigint NOT NULL UNIQUE REFERENCES bands(id) ON DELETE CASCADE,
  provider             text NOT NULL CHECK (provider IN ('pcloud')),
  link                 text NOT NULL,
  code                 text NOT NULL,
  api_host             text NOT NULL,
  name                 text NOT NULL DEFAULT '',
  synced_at            timestamptz,
  created_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  created_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE band_recordings (
  id           bigserial PRIMARY KEY,
  source_id    bigint NOT NULL REFERENCES band_recording_sources(id) ON DELETE CASCADE,
  band_id      bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  file_id      bigint NOT NULL,          -- the provider's
  path         text NOT NULL,            -- its folder, from the shared one
  name         text NOT NULL,
  size         bigint NOT NULL DEFAULT 0,
  content_type text NOT NULL DEFAULT '',
  created      timestamptz,
  modified     timestamptz,
  kind         text NOT NULL CHECK (kind IN ('rehearsal', 'concert', 'audition', 'studio', 'album', 'setlist', 'other')),
  folder_date  text NOT NULL DEFAULT '', -- the date its folder says: '2025-03-13', '2023-12'
  file_date    text NOT NULL DEFAULT '', -- the date its name says
  session_date date,                     -- its session's date, corrections applied
  position     int,                      -- its place in the session: "07 - ..."
  title        text NOT NULL DEFAULT '', -- the song, as the file names it
  take         int,
  variant_id   bigint REFERENCES songs(id) ON DELETE SET NULL,
  gone         boolean NOT NULL DEFAULT false, -- not in the folder at the last sync
  UNIQUE (source_id, file_id)
);
CREATE INDEX band_recordings_session_idx ON band_recordings (band_id, kind, session_date);
CREATE INDEX band_recordings_song_idx ON band_recordings (variant_id) WHERE variant_id IS NOT NULL;

-- A band's corrections to what its file names say: a date written wrong
-- ('2015-01-27' for '2025-01-27', a folder dated the wrong year), a title
-- spelt another way ('Kawazaki'). Applied at every sync.
CREATE TABLE band_recording_fixes (
  id         bigserial PRIMARY KEY,
  band_id    bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  kind       text NOT NULL CHECK (kind IN ('date', 'title')),
  from_value text NOT NULL,
  to_value   text NOT NULL,             -- a date; for a title, a song's id
  created_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (band_id, kind, from_value)
);

-- Each member's stars for a recording: ½ to 5, by halves.
CREATE TABLE band_recording_ratings (
  recording_id bigint NOT NULL REFERENCES band_recordings(id) ON DELETE CASCADE,
  person_id    bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  stars        numeric(2,1) NOT NULL CHECK (stars >= 0.5 AND stars <= 5 AND stars * 2 = floor(stars * 2)),
  rated_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (recording_id, person_id)
);

-- The recording a band chose as its reference for a song: what its
-- playlist plays, chosen by hand among the song's recordings.
CREATE TABLE band_song_recordings (
  band_id      bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  variant_id   bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  recording_id bigint NOT NULL REFERENCES band_recordings(id) ON DELETE CASCADE,
  chosen_by_person_id bigint REFERENCES people(id) ON DELETE SET NULL,
  chosen_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (band_id, variant_id)
);
