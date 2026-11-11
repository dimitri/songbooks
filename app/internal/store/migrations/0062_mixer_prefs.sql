-- Each person's own Mixer settings for a song (track volumes, mute/solo,
-- zoom, speed, loop) — a listening preference, never song data, so one
-- JSON document per (person, variant) the client owns the shape of.
CREATE TABLE mixer_prefs (
  person_id bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  variant_id bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  prefs jsonb NOT NULL DEFAULT '{}'::jsonb,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (person_id, variant_id)
);
