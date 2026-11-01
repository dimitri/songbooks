-- Jam mode previously had no explicit "songs in this jam" concept —
-- ListJamSongs just listed every song in the event's whole project. This
-- adds the missing per-jam song list (jam_songs) that per-instrument
-- headcount caps (jam_song_instrument_limits) and the organizer's play
-- order (Phase 5) both need to attach to: "this song, in this jam," not
-- globally across the project.
--
-- play_order lives on jam_songs now rather than being added by a later
-- ALTER TABLE once Phase 5's organizer-ordering work lands — the column
-- belongs on this table either way, and the organizer UI (Phase 2) already
-- needs a stable, settable order for the pickable song list itself.
CREATE TABLE jam_songs (
  jam_event_id bigint NOT NULL REFERENCES jam_events(id) ON DELETE CASCADE,
  song_id      bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  play_order   integer NOT NULL DEFAULT 0,
  added_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (jam_event_id, song_id)
);
CREATE INDEX jam_songs_event_order_idx ON jam_songs (jam_event_id, play_order);

-- Per-(jam event, song, instrument) headcount caps. max_count NULL means
-- "no cap set" — any custom instrument beyond the four organizer-facing
-- defaults (drummer/bassist/guitarist/singer) starts this way: min 0, no
-- max, unless the organizer explicitly sets one.
CREATE TABLE jam_song_instrument_limits (
  jam_event_id bigint NOT NULL,
  song_id      bigint NOT NULL,
  instrument   text NOT NULL,
  min_count    integer NOT NULL DEFAULT 0,
  max_count    integer,
  PRIMARY KEY (jam_event_id, song_id, instrument),
  FOREIGN KEY (jam_event_id, song_id) REFERENCES jam_songs (jam_event_id, song_id) ON DELETE CASCADE,
  CONSTRAINT jam_song_instrument_limits_max_nonneg CHECK (max_count IS NULL OR max_count >= 0),
  CONSTRAINT jam_song_instrument_limits_min_nonneg CHECK (min_count >= 0)
);
