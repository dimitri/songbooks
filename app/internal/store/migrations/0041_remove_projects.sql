-- Remove the "project" notion entirely. A song variant's storage address
-- used to be (project_id, stem); from here on a variant is addressed by
-- its own id only, and ownership is exactly one of: the shared public
-- catalogue (visibility='global'), a band (owner_band_id), a person
-- (owner_person_id), or — new in this migration — a specific setlist
-- (owner_setlist_id, for a setlist-local edit). Every other table that
-- pointed at projects (bands, setlists, jam_events, project_albums,
-- invites, project_members) is repointed or retired in the same pass,
-- since they can't be left half-migrated once `projects` itself is
-- dropped at the end of this file.
--
-- This migration also carries real, one-time data decisions for this
-- specific deployment's existing content (documented inline below) —
-- not a generic, reusable transform. That's deliberately in character
-- for a single-deployment app (see mp3-reference-audio.md's own
-- "decisions made for this deployment" framing), not a pattern to
-- repeat for a multi-tenant product.

ALTER TABLE songs ADD COLUMN owner_setlist_id bigint REFERENCES setlists(id);

-- Real-data ownership decision (user's own call): a project-linked
-- band's existing songs follow its music_type. Mercy (covers) keeps its
-- 43 songs as public-catalogue ('global') content — a covers band's
-- repertoire is the kind of thing meant to be shared/published. Other
-- Lies (original) gets its 30 songs switched to band-private ownership
-- — an originals band's material defaults to NOT published, matching
-- "probably won't publish" in the brief. Either way, every one of those
-- songs also gets an explicit songbook_entries pointer into its band's
-- default songbook, same as a directly band-owned song already has
-- (see migration 0039 — this mirrors FourNonBlondes/"What's Up"'s
-- existing owner+pointer pattern instead of inventing a second one).
INSERT INTO songbook_entries (songbook_id, variant_id)
SELECT sb.id, s.id
FROM songs s
JOIN projects p ON p.id = s.project_id
JOIN songbooks sb ON sb.owner_kind = 'band' AND sb.is_default
WHERE p.slug = 'Mercy' AND sb.owner_band_id = 14;

UPDATE songs SET visibility = 'band', owner_band_id = 15
WHERE project_id = (SELECT id FROM projects WHERE slug = 'OtherLies');

INSERT INTO songbook_entries (songbook_id, variant_id)
SELECT sb.id, s.id
FROM songs s
JOIN songbooks sb ON sb.owner_kind = 'band' AND sb.is_default
WHERE s.owner_band_id = 15 AND sb.owner_band_id = 15;

-- BandsCamp (66 songs) and the two dbgsong* projects (1 song each) were
-- already visibility='global' with no band — they stay exactly that:
-- plain public-catalogue content, no songbook pointer needed (the
-- catalogue isn't itself a songbook). FourNonBlondes's 7 songs were
-- already owner_band_id=16/visibility='band' with their songbook
-- pointer already carried over in migration 0039 — nothing left to do
-- for either.

-- The one legacy project-scoped setlist (id=1, "Saturday Night", drawn
-- from both Mercy's and Other Lies' repertoires) becomes band-scoped
-- under Mercy (the band actually playing the gig) with its entries
-- rewritten from "book/stem" to "variant:N" — the same syntax every
-- person/band-scoped setlist already uses (see internal/setlist's
-- parser). Variant ids resolved by hand against this deployment's real
-- data before writing this migration.
UPDATE setlists SET
  scope = 'band', owner_band_id = 14, project_id = NULL, stem = '',
  body = 'title: Saturday Night
date: 2026-11-07
venue: The Loft

[Set 1]
- variant:86
- variant:101
  note: capo 2
- variant:82
- variant:95

[Encore]
- variant:106

[Set 2]
- variant:123
'
WHERE id = 1;

ALTER TABLE setlists DROP CONSTRAINT setlists_scope_owner_check;
ALTER TABLE setlists DROP COLUMN project_id;
ALTER TABLE setlists DROP COLUMN stem;
ALTER TABLE setlists ADD CONSTRAINT setlists_scope_owner_check CHECK (
  (scope = 'person' AND owner_person_id IS NOT NULL AND owner_band_id IS NULL) OR
  (scope = 'band'   AND owner_band_id IS NOT NULL   AND owner_person_id IS NULL)
);
-- 'project' stays a dead, unused value on the setlist_scope enum —
-- Postgres can't drop an enum value, same situation migrations
-- 0011/0014/0024 already accepted for variant_visibility.

-- jam_events: repoint from a project to a songbook (the pool of songs a
-- jam event's "add a song" step offers/creates into). The one real jam
-- event (project_id=1, BandsCamp) maps to BandsCamp's owner's own
-- default songbook — BandsCamp has a single person_id=1 ('tapoueh')
-- owner and no band, so there's no band songbook to use instead.
ALTER TABLE jam_events ADD COLUMN songbook_id bigint REFERENCES songbooks(id);
UPDATE jam_events je SET songbook_id = sb.id
FROM songbooks sb
WHERE sb.owner_kind = 'person' AND sb.owner_person_id = 1 AND sb.is_default
  AND je.project_id = (SELECT id FROM projects WHERE slug = 'BandsCamp');
ALTER TABLE jam_events ALTER COLUMN songbook_id SET NOT NULL;
ALTER TABLE jam_events DROP COLUMN project_id;

-- project_albums -> band_albums: all 3 existing rows belong to Other
-- Lies (project_id=3) -> band 15. Album-grouped repertoire display is
-- already a band-repertoire feature (band_catalog.html's AlbumGroups);
-- this just repoints its storage key the same way everything else here
-- moves from project to band/person.
ALTER TABLE project_albums RENAME TO band_albums;
ALTER TABLE band_albums RENAME COLUMN project_id TO band_id;
UPDATE band_albums SET band_id = 15 WHERE band_id = 3;
ALTER TABLE band_albums DROP CONSTRAINT project_albums_pkey;
ALTER TABLE band_albums ADD PRIMARY KEY (band_id, album);
ALTER TABLE band_albums DROP CONSTRAINT project_albums_project_id_fkey;
ALTER TABLE band_albums ADD CONSTRAINT band_albums_band_id_fkey FOREIGN KEY (band_id) REFERENCES bands(id) ON DELETE CASCADE;

-- Project membership (owner/editor/viewer) has no replacement: a band's
-- access control is band_members (core/guest, already built), and a
-- person's own songbook is single-owner by construction — no separate
-- ACL/invite mechanism is needed for it. The one real invite row had
-- project_id/role both NULL already (a "plain" account-creation link,
-- see migration 0019) — dropping the project-invite columns loses no
-- real data.
-- Carry over each project's real, hand-curated song_order (Mercy's
-- Set 1/2/3/Unassigned groupings in particular are real curated data,
-- not throwaway) into its new owner's songbook, with stems resolved to
-- variant ids while songs.project_id/stem still exist to resolve them
-- against. BandsCamp has no band, so its order lands on its sole
-- person-owner's (person_id=1, 'tapoueh') default songbook instead.
DO $$
DECLARE
  proj RECORD;
  grp JSONB;
  new_groups JSONB;
  stem TEXT;
  vid BIGINT;
  var_ids JSONB;
  sb_id BIGINT;
BEGIN
  FOR proj IN SELECT id, slug, song_order FROM projects WHERE song_order::text != '[]' LOOP
    sb_id := NULL;
    IF proj.slug = 'Mercy' THEN
      SELECT id INTO sb_id FROM songbooks WHERE owner_kind = 'band' AND owner_band_id = 14 AND is_default;
    ELSIF proj.slug = 'OtherLies' THEN
      SELECT id INTO sb_id FROM songbooks WHERE owner_kind = 'band' AND owner_band_id = 15 AND is_default;
    ELSIF proj.slug = 'BandsCamp' THEN
      SELECT id INTO sb_id FROM songbooks WHERE owner_kind = 'person' AND owner_person_id = 1 AND is_default;
    END IF;
    IF sb_id IS NULL THEN
      CONTINUE;
    END IF;

    new_groups := '[]'::jsonb;
    FOR grp IN SELECT * FROM jsonb_array_elements(proj.song_order) LOOP
      var_ids := '[]'::jsonb;
      FOR stem IN SELECT * FROM jsonb_array_elements_text(grp->'Stems') LOOP
        SELECT s.id INTO vid FROM songs s WHERE s.project_id = proj.id AND s.stem = stem;
        IF vid IS NOT NULL THEN
          var_ids := var_ids || to_jsonb(vid);
        END IF;
      END LOOP;
      new_groups := new_groups || jsonb_build_array(jsonb_build_object('Label', grp->>'Label', 'VariantIDs', var_ids));
    END LOOP;

    UPDATE songbooks SET song_order = new_groups WHERE id = sb_id;
  END LOOP;
END $$;

-- Mercy's own real playlist_url (a real YouTube playlist link — the
-- only project that has one set) carries over to its band songbook.
UPDATE songbooks SET playlist_url = (SELECT playlist_url FROM projects WHERE slug = 'Mercy')
WHERE owner_kind = 'band' AND owner_band_id = 14 AND is_default;

ALTER TABLE invites DROP COLUMN project_id;
ALTER TABLE invites DROP COLUMN role;

DROP TABLE project_members;
DROP TYPE project_role;

ALTER TABLE songs DROP COLUMN project_id;
ALTER TABLE songs DROP COLUMN stem;

ALTER TABLE bands DROP COLUMN project_id;

DROP TABLE projects;
