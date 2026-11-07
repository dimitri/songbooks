-- The click-track page's own metronome display options (visual size,
-- beat-group, portrait/landscape, every-beat/beat-1-only) are a silent,
-- per-person profile preference — recalled on whatever device the
-- person next opens the page on, not just the browser that last set it
-- (localStorage alone only covers that one browser).
ALTER TABLE people ADD COLUMN click_track_prefs jsonb NOT NULL DEFAULT '{}'::jsonb;
