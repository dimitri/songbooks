-- The ownership model, step 3: a set list's songs in a table
-- (docs/ownership-model.md §4, §9). A set list lists songs of its owner's
-- songbook, a specific version when the owner has several; its lines move
-- out of the text body ("[Set 1]", "- variant:123", "  note: …") into
-- setlist_items, one row per line, in order:
--
--   - a heading row ("Set 1", "Encore": section set, variant_id NULL)
--     starts a set; the songs after it, up to the next heading, are that
--     set's. Headings are rows, not a column on each song, so a set with
--     no song yet ("[Encore]" waiting for its songs) is kept, two sets
--     may have the same name, and moving a song across sets is changing
--     its position, nothing else;
--   - a song row (variant_id set) is one song of the set list — the
--     version picked for this set list when the owner's songbook has
--     several — with its note for the gig ("capo 2, watch the bridge").
--
-- The body's header lines go to columns: "title:" is the set list's
-- title (what its page showed first), "date:" and "venue:" new columns
-- (free text, as they were). The body column goes: everything reads the
-- rows. A set list's revision snapshot keeps the text form
-- (internal/setlist's Write of the rows), so the history recorded before
-- this migration still reverts.

ALTER TABLE setlists
  ADD COLUMN date text NOT NULL DEFAULT '',
  ADD COLUMN venue text NOT NULL DEFAULT '';

CREATE TABLE setlist_items (
  id         bigserial PRIMARY KEY,
  setlist_id bigint NOT NULL REFERENCES setlists(id) ON DELETE CASCADE,
  position   int NOT NULL,
  section    text,
  variant_id bigint REFERENCES songs(id) ON DELETE CASCADE,
  note       text NOT NULL DEFAULT '',
  UNIQUE (setlist_id, position),
  CONSTRAINT setlist_items_heading_or_song CHECK (num_nonnulls(section, variant_id) = 1),
  CONSTRAINT setlist_items_note_on_songs CHECK (variant_id IS NOT NULL OR note = '')
);
CREATE INDEX setlist_items_variant_idx ON setlist_items (variant_id) WHERE variant_id IS NOT NULL;

-- setlist_body_rows reads a set list body the way internal/setlist's
-- Parse does (a test checks they agree on real-shaped bodies): header
-- "key: value" lines (title, date, venue) until the first "[Label]", then
-- "- variant:N" songs, each with an optional "note:" line after it;
-- "#" lines are comments. Rows: kind 'title' / 'date' / 'venue' (label
-- the value, pos 0), 'section' (label), 'song' (variant_id, note), and
-- 'other' for a "- book/stem" line (the project-scoped set lists, gone
-- since migration 0041: none left), which a set list can't hold any more.
CREATE FUNCTION setlist_body_rows(body text)
RETURNS TABLE (pos int, kind text, label text, variant_id bigint, note text)
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
  ws constant text := E' \t\n\r\f' || chr(11) || chr(133) || chr(160); -- chr(11) is \v: PostgreSQL's E'' strings don't know \v (it reads as a plain v)
  raw text;
  line text;
  ref text;
  num text;
  in_head boolean := true;
  in_set boolean := false;
  set_entries int := 0;
  meta_title text;
  meta_date text;
  meta_venue text;
  kinds text[] := '{}';
  labels text[] := '{}';
  ids bigint[] := '{}';
  notes text[] := '{}';
  i int;
BEGIN
  FOREACH raw IN ARRAY string_to_array(coalesce(body, ''), E'\n') LOOP
    line := btrim(raw, ws);
    CONTINUE WHEN left(line, 1) = '#';
    IF left(line, 1) = '[' AND right(line, 1) = ']' AND length(line) >= 2 THEN
      kinds := kinds || 'section'::text;
      labels := labels || substr(line, 2, length(line) - 2);
      ids := ids || NULL::bigint;
      notes := notes || ''::text;
      in_head := false;
      in_set := true;
      set_entries := 0;
      CONTINUE;
    END IF;
    IF in_head THEN
      CONTINUE WHEN line = '' OR strpos(line, ':') = 0;
      CASE btrim(split_part(line, ':', 1), ws)
        WHEN 'title' THEN meta_title := btrim(substr(line, strpos(line, ':') + 1), ws);
        WHEN 'date' THEN meta_date := btrim(substr(line, strpos(line, ':') + 1), ws);
        WHEN 'venue' THEN meta_venue := btrim(substr(line, strpos(line, ':') + 1), ws);
        ELSE NULL;
      END CASE;
      CONTINUE;
    END IF;
    CONTINUE WHEN NOT in_set OR line = '';
    IF left(line, 2) = '- ' THEN
      ref := btrim(substr(line, 3), ws);
      IF left(ref, 8) = 'variant:' THEN
        num := btrim(substr(ref, 9), ws);
        IF num ~ '^[+]?[0-9]+$' AND num::numeric BETWEEN 1 AND 9223372036854775807 THEN
          kinds := kinds || 'song'::text;
          labels := labels || NULL::text;
          ids := ids || num::bigint;
          notes := notes || ''::text;
          set_entries := set_entries + 1;
        END IF;
        CONTINUE;
      END IF;
      kinds := kinds || 'other'::text;
      labels := labels || ref;
      ids := ids || NULL::bigint;
      notes := notes || ''::text;
      set_entries := set_entries + 1;
    ELSIF left(line, 5) = 'note:' AND set_entries > 0 THEN
      notes[cardinality(notes)] := btrim(substr(line, 6), ws);
    END IF;
  END LOOP;

  IF meta_title IS NOT NULL THEN
    pos := 0; kind := 'title'; label := meta_title; variant_id := NULL; note := ''; RETURN NEXT;
  END IF;
  IF meta_date IS NOT NULL THEN
    pos := 0; kind := 'date'; label := meta_date; variant_id := NULL; note := ''; RETURN NEXT;
  END IF;
  IF meta_venue IS NOT NULL THEN
    pos := 0; kind := 'venue'; label := meta_venue; variant_id := NULL; note := ''; RETURN NEXT;
  END IF;
  FOR i IN 1 .. coalesce(cardinality(kinds), 0) LOOP
    pos := i; kind := kinds[i]; label := labels[i]; variant_id := ids[i]; note := notes[i];
    RETURN NEXT;
  END LOOP;
END $$;

-- Every set list's lines, in order. A line whose song is gone (deleted
-- since) has nothing to point at: it goes, as every page skipped it.
INSERT INTO setlist_items (setlist_id, position, section, variant_id, note)
SELECT sl.id,
       row_number() OVER (PARTITION BY sl.id ORDER BY r.pos),
       CASE WHEN r.kind = 'section' THEN r.label END,
       CASE WHEN r.kind = 'song' THEN r.variant_id END,
       CASE WHEN r.kind = 'song' THEN r.note ELSE '' END
FROM setlists sl
CROSS JOIN LATERAL setlist_body_rows(sl.body) r
WHERE r.kind = 'section'
   OR (r.kind = 'song' AND EXISTS (SELECT 1 FROM songs s WHERE s.id = r.variant_id));

UPDATE setlists sl SET
  title = coalesce(nullif((SELECT r.label FROM setlist_body_rows(sl.body) r WHERE r.kind = 'title'), ''), sl.title),
  date  = coalesce((SELECT r.label FROM setlist_body_rows(sl.body) r WHERE r.kind = 'date'), ''),
  venue = coalesce((SELECT r.label FROM setlist_body_rows(sl.body) r WHERE r.kind = 'venue'), '');

DO $$
DECLARE
  n_lists int; n_songs int; n_sections int; n_notes int; n_gone int; n_other int;
BEGIN
  SELECT count(*) INTO n_lists FROM setlists;
  SELECT count(*) FILTER (WHERE variant_id IS NOT NULL), count(*) FILTER (WHERE section IS NOT NULL),
         count(*) FILTER (WHERE note <> '')
    INTO n_songs, n_sections, n_notes FROM setlist_items;
  SELECT count(*) FILTER (WHERE r.kind = 'song' AND NOT EXISTS (SELECT 1 FROM songs s WHERE s.id = r.variant_id)),
         count(*) FILTER (WHERE r.kind = 'other')
    INTO n_gone, n_other
    FROM setlists sl CROSS JOIN LATERAL setlist_body_rows(sl.body) r;
  RAISE NOTICE 'setlist_items: % set lists, % songs (% with a note), % sets; % lines of a deleted song and % book/stem lines left out',
    n_lists, n_songs, n_notes, n_sections, n_gone, n_other;
END $$;

ALTER TABLE setlists DROP COLUMN body;
