-- The language of the app (docs/i18n.md): a member's own choice, else
-- the community's or band's on its pages, else the browser's. '' leaves
-- it to the next.
ALTER TABLE people ADD COLUMN locale text NOT NULL DEFAULT '' CHECK (locale IN ('', 'en', 'fr'));
ALTER TABLE communities ADD COLUMN locale text NOT NULL DEFAULT '' CHECK (locale IN ('', 'en', 'fr'));
ALTER TABLE bands ADD COLUMN locale text NOT NULL DEFAULT '' CHECK (locale IN ('', 'en', 'fr'));
