-- Where a recording's sync came from (docs/mp3-reference-audio.md, "Sync
-- state"): 'auto' when a program placed it (the import wizard's
-- proposal, the Mixer's Auto-align) and nobody has checked it by ear yet,
-- 'set' when a person placed or confirmed it (a tap in the Sync sheet, a
-- flag dragged in the Mixer, a Guitar Pro file's own sync points). The
-- player says which: "Synchro estimée" or "Synchro réglée".
ALTER TABLE song_audio_markers
    ADD COLUMN origin text NOT NULL DEFAULT 'set' CHECK (origin IN ('auto', 'set'));
ALTER TABLE gp_bar_sync_point
    ADD COLUMN origin text NOT NULL DEFAULT 'set' CHECK (origin IN ('auto', 'set'));

-- The markers saved so far don't say. A recording whose markers were all
-- written within the same second came from one request: the wizard's
-- "Save and finish" (or a whole-grid move in the Mixer). Those are
-- estimates until someone confirms them; markers written over time were
-- placed by hand.
UPDATE song_audio_markers m SET origin = 'auto'
FROM (
    SELECT song_audio_id FROM song_audio_markers
    GROUP BY song_audio_id
    HAVING count(*) >= 2 AND max(created_at) - min(created_at) < interval '1 second'
) batch
WHERE m.song_audio_id = batch.song_audio_id;

-- Each sync saved as a whole (the Sync sheet, the wizard, Auto-align)
-- keeps the one it replaced, so a bad save can be taken back.
CREATE TABLE song_audio_sync_history (
    id            bigserial PRIMARY KEY,
    song_audio_id bigint NOT NULL REFERENCES song_audio(id) ON DELETE CASCADE,
    saved_at      timestamptz NOT NULL DEFAULT now(),
    saved_by      bigint REFERENCES people(id) ON DELETE SET NULL,
    -- [{"sectionIndex": 0, "timeSeconds": 10.45, "origin": "auto"}, ...]
    markers       jsonb NOT NULL
);
CREATE INDEX song_audio_sync_history_audio_idx ON song_audio_sync_history (song_audio_id, saved_at DESC);
