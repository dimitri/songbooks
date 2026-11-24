-- No usernames (docs/accounts.md): 0135 made people.username nullable
-- and the app stopped reading and writing it. Nothing depends on it any
-- more (no view, function, index or constraint but its own UNIQUE, which
-- goes with it; sbdemo, songimport and the deploy scripts don't name
-- it), so the column and the old login names in it go.
--
-- No CASCADE: should anything still depend on it, the migration fails
-- and says what, rather than silently dropping it too.
ALTER TABLE people DROP COLUMN IF EXISTS username;
