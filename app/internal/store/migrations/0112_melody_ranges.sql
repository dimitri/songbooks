-- A recording's melody range (docs/coaching.md, "The song's melody over
-- the student's range"), from its vocals part (audioanalysis
-- MelodyRangeOf: the voice tracked note by note, its held notes' 5th to
-- 95th percentiles), found in the background by the app
-- (httpapi/melody_ranges.go), once per split: a new split of the
-- recording (a new vocals part: stem_created_at) or a new analyzer finds
-- it again. ok false: too little singing to tell (or the part couldn't be
-- read), kept so it isn't tried again. (0111 is coach-books' voice
-- labels; the highest in review.)
CREATE TABLE song_melody_ranges (
  song_audio_id   bigint PRIMARY KEY REFERENCES song_audio(id) ON DELETE CASCADE,
  stem_sha256     text NOT NULL,
  stem_created_at timestamptz NOT NULL,
  analyzer        text NOT NULL,
  ok              boolean NOT NULL,
  low_midi        int,
  high_midi       int,
  voiced_seconds  real NOT NULL DEFAULT 0,
  took_seconds    real NOT NULL DEFAULT 0,
  created_at      timestamptz NOT NULL DEFAULT now()
);
