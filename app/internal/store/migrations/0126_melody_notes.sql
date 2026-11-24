-- The lead voice's notes, from a recording's vocals part
-- (docs/melody-sheet.md; audioanalysis MelodyNotesOf: the voice tracked
-- note by note, cut into notes), found in the background by the app
-- (httpapi/melody_sheet.go), once per split and analyzer, as the melody
-- range is (0112): a new split of the recording (stem_created_at) or a
-- new analyzer finds them again. notes: [[start, end, midi, conf], ...] in
-- seconds and fractional MIDI; ok false: too little singing (or the part
-- couldn't be read), kept so it isn't tried again. Placing them in bars
-- and under the lyrics is done at request time (internal/melody): it
-- follows the sync and the chart, which change without the recording.
-- (0125 is catalogue-edits'.)
CREATE TABLE song_melody_notes (
  song_audio_id   bigint PRIMARY KEY REFERENCES song_audio(id) ON DELETE CASCADE,
  stem_sha256     text NOT NULL,
  stem_created_at timestamptz NOT NULL,
  analyzer        text NOT NULL,
  ok              boolean NOT NULL,
  notes           jsonb NOT NULL DEFAULT '[]',
  voiced_seconds  real NOT NULL DEFAULT 0,
  took_seconds    real NOT NULL DEFAULT 0,
  created_at      timestamptz NOT NULL DEFAULT now()
);

-- An editor's correction of where a lyric line's syllables start on the
-- melody: so many notes left (-) or right (+) of where they were laid.
-- Kept by the song (its lyrics are the song's, whichever recording the
-- melody comes from) and the line (section index, line index in it), with
-- the line's text: a correction made for a line that has changed since
-- no longer applies.
CREATE TABLE song_melody_lyric_shifts (
  variant_id    bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  section_index int NOT NULL,
  line_index    int NOT NULL,
  line_text     text NOT NULL,
  shift         int NOT NULL,
  updated_by    bigint REFERENCES people(id) ON DELETE SET NULL,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (variant_id, section_index, line_index)
);
