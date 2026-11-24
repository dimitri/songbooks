-- Two songs in another key, kept in a songbook (the same song, no copy;
-- see internal/store/songbook_keys.go), as the user asked:
--   * Still Got the Blues, 2 semitones down, in the user's own songbook
--     (a guitar tuned to D standard);
--   * Just The Way You Are, 3 semitones down, in the vocal coaching
--     student songbook "Les morceaux de Dimitri".
-- Which version of each song to point at is the user's call (duplicates):
-- the song ids are parameters. Run it AFTER the code that reads these keys
-- is deployed.
--
-- It shows the rows before and after, and rolls back unless -v apply=1:
--
--   psql "$DATABASE_URL" -v person=1 -v blues=ID \
--        -v student_songbook=18 -v just_the_way=ID              # dry run
--   psql ... -v apply=1                                          # do it
--
-- What it changes: at most two songbook_entries rows — the user's default
-- songbook's entry for :blues (added when it isn't there) set to -2, and
-- :student_songbook's entry for :just_the_way (added when it isn't there)
-- set to -3. No song changes.

\set ON_ERROR_STOP on
\if :{?person}
\else
  \echo 'missing -v person=<person id>'
  \quit
\endif
\if :{?blues}
\else
  \echo 'missing -v blues=<song id>'
  \quit
\endif
\if :{?student_songbook}
\else
  \echo 'missing -v student_songbook=<songbook id> (18: "Les morceaux de Dimitri")'
  \quit
\endif
\if :{?just_the_way}
\else
  \echo 'missing -v just_the_way=<song id> (221 or 328)'
  \quit
\endif

BEGIN;

CREATE TEMP TABLE wanted ON COMMIT DROP AS
SELECT sb.id AS songbook_id, sb.name AS songbook, s.id AS variant_id, s.title, s.visibility::text AS visibility, k.semitones, p.id AS person_id
FROM (VALUES
  ((SELECT id FROM songbooks WHERE owner_kind = 'person' AND is_default
     AND owner_person_id = :person::bigint), :blues::bigint, -2),
  (:student_songbook::bigint, :just_the_way::bigint, -3)
) AS k(songbook_id, variant_id, semitones)
JOIN songbooks sb ON sb.id = k.songbook_id
JOIN songs s ON s.id = k.variant_id
LEFT JOIN people p ON p.id = :person::bigint;

\echo 'what it sets (songbook, song, semitones):'
SELECT songbook_id, songbook, variant_id, title, visibility, semitones FROM wanted ORDER BY songbook_id;
\echo 'before (no row: the song is not in that songbook yet):'
SELECT se.songbook_id, se.variant_id, se.semitones FROM songbook_entries se
JOIN wanted w ON w.songbook_id = se.songbook_id AND w.variant_id = se.variant_id;

SELECT count(*) = 2 AS both_found FROM wanted \gset
\if :both_found
\else
  \echo 'a songbook or a song was not found: nothing changed'
  ROLLBACK;
  \quit
\endif

INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id, semitones)
SELECT songbook_id, variant_id, person_id, semitones FROM wanted
ON CONFLICT (songbook_id, variant_id) DO UPDATE SET semitones = EXCLUDED.semitones;

\echo 'after:'
SELECT se.songbook_id, se.variant_id, se.semitones FROM songbook_entries se
JOIN wanted w ON w.songbook_id = se.songbook_id AND w.variant_id = se.variant_id;

\if :{?apply}
  COMMIT;
  \echo 'applied'
\else
  ROLLBACK;
  \echo 'dry run: rolled back (-v apply=1 to keep it)'
\endif
