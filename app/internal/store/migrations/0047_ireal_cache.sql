-- A local cache of iReal Pro forum chord charts, fetched ahead of time
-- rather than searched live (the format is too fiddly — pick a forum
-- thread, extract a scrambled irealb:// link, decode it — for a live
-- per-request search the way Ultimate Guitar's importer does). Two
-- layers: the raw fetched bytes (so a parser bug can be fixed later
-- without re-fetching the forum), and a normalized per-song catalog
-- derived from them, which is what title/artist lookups actually query.

CREATE TABLE ireal_library_fetches (
    id          bigserial PRIMARY KEY,
    source_url  text NOT NULL,
    title       text NOT NULL DEFAULT '',   -- the forum thread's own title, best-effort
    raw_body    text NOT NULL,              -- the exact irealb:// link text as fetched
    fetched_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ireal_library_fetches_source_url_idx ON ireal_library_fetches (source_url);

CREATE TABLE ireal_songs (
    id              bigserial PRIMARY KEY,
    fetch_id        bigint NOT NULL REFERENCES ireal_library_fetches(id) ON DELETE CASCADE,
    title           text NOT NULL,
    composer        text NOT NULL DEFAULT '',
    style           text NOT NULL DEFAULT '',
    key             text NOT NULL DEFAULT '',
    time_sig_num    int NOT NULL DEFAULT 4,
    time_sig_den    int NOT NULL DEFAULT 4,
    grid_text       text NOT NULL,          -- this app's own grid: line text (song.SimplifyGridText output)
    reaction_count  int NOT NULL DEFAULT 0, -- the forum post's reaction count, a weak popularity signal
    norm_title      text GENERATED ALWAYS AS (lower(trim(title))) STORED,
    norm_composer   text GENERATED ALWAYS AS (lower(trim(composer))) STORED,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ireal_songs_norm_title_idx ON ireal_songs (norm_title);
CREATE INDEX ireal_songs_fetch_idx ON ireal_songs (fetch_id);
