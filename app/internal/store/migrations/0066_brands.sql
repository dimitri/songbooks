-- A community's or band's look (internal/brand): colors, fonts, shape,
-- logo mark, tagline, website — applied to its pages and to the songs,
-- songbooks and setlists seen in its context. '{}' is the app's own look.
ALTER TABLE communities ADD COLUMN brand jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE bands ADD COLUMN brand jsonb NOT NULL DEFAULT '{}'::jsonb;
