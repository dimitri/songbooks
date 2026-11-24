-- The ownership model, steps 4–5 (docs/ownership-model.md §3, §7).
--
-- song_merges: a duplicate merged into another by The Big Songbook's
-- staff (§7). The merged song goes (everything that pointed at it points
-- at the one kept: songbook entries, set list lines, recordings,
-- versions' forked_from_id); this row keeps where it went, so an old link
-- to it (/song/59) opens the one kept, and its history (its revisions,
-- kept under its own id) is listed on the kept song's.
CREATE TABLE song_merges (
  merged_song_id bigint PRIMARY KEY,  -- no foreign key: the song is gone
  into_song_id   bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  title          text NOT NULL,
  artist         text NOT NULL,
  version_name   text NOT NULL DEFAULT '',
  owner_name     text NOT NULL DEFAULT '',
  reason         text NOT NULL DEFAULT '',   -- 'title', 'video', or both: 'title,video'
  moved          jsonb NOT NULL DEFAULT '{}', -- what moved (store.RepointCounts)
  merged_by      bigint REFERENCES people(id) ON DELETE SET NULL,
  merged_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX song_merges_into_idx ON song_merges (into_song_id);

-- song_recording_fit: whether a song follows its reference recording
-- (§3, flags only: a badge or a warning, never a rule), as the
-- recording's analysis (migration 0106) laid on the chart says
-- (songimport.RecordingFit). Kept for the song's version and the
-- analysis it was read from: a new revision or a new analysis reads it
-- again.
CREATE TABLE song_recording_fit (
  song_id       bigint PRIMARY KEY REFERENCES songs(id) ON DELETE CASCADE,
  song_version  integer NOT NULL,
  song_audio_id bigint NOT NULL REFERENCES song_audio(id) ON DELETE CASCADE,
  source_sha256 text NOT NULL,
  fit           text NOT NULL CHECK (fit IN ('follows', 'differs', 'unknown')),
  detail        text NOT NULL DEFAULT '',
  checked_at    timestamptz NOT NULL DEFAULT now()
);
