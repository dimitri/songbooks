-- The ownership model, step 2: one access rule (docs/ownership-model.md
-- §2, §4, §5). The app reads songs.owner_* and songs.public now, never
-- variant_visibility, which goes.
--
-- First what cmd/ownership's catalogue rows do, for a database where it
-- hasn't run yet (a fresh one, or a deploy before the mapping): a song
-- with no owner is the catalogue's, The Big Songbook's, public; a set
-- list's song (owner_setlist_id, none on staging) is its set list's
-- owner's. Then every song has exactly one owner.
UPDATE songs SET owner_community_id = big_songbook_id()
WHERE owner_person_id IS NULL AND owner_band_id IS NULL AND owner_community_id IS NULL
  AND owner_setlist_id IS NULL;
UPDATE songs s SET owner_person_id = sl.owner_person_id, owner_band_id = sl.owner_band_id,
                   owner_community_id = sl.owner_community_id
FROM setlists sl
WHERE s.owner_setlist_id = sl.id
  AND s.owner_person_id IS NULL AND s.owner_band_id IS NULL AND s.owner_community_id IS NULL;
UPDATE songs SET owner_person_id = created_by_person_id
WHERE owner_person_id IS NULL AND owner_band_id IS NULL AND owner_community_id IS NULL
  AND created_by_person_id IS NOT NULL;
UPDATE songs SET owner_community_id = big_songbook_id()
WHERE owner_person_id IS NULL AND owner_band_id IS NULL AND owner_community_id IS NULL;
UPDATE songs SET public = true
WHERE visibility = 'global' AND owner_community_id = big_songbook_id() AND NOT public;

ALTER TABLE songs ADD CONSTRAINT songs_one_owner
  CHECK (num_nonnulls(owner_person_id, owner_band_id, owner_community_id) = 1);
ALTER TABLE songs DROP COLUMN owner_setlist_id;
ALTER TABLE songs DROP COLUMN visibility;
DROP TYPE variant_visibility;

-- closed replaces shared (migration 0072): a songbook that wasn't shared
-- is closed, as cmd/ownership makes it.
UPDATE songbooks SET closed = true, public = false WHERE NOT shared AND NOT closed;
ALTER TABLE songbooks DROP COLUMN shared;

-- song_visible says whether person sees song: the rule's "see"
-- (store.SongRightsFor), here so a list's query and a page's check never
-- disagree. A public song: everyone signed in. A private one: its owner's
-- members — the person; a band's current members, core or guest; a
-- community's current members, except that a song whose community
-- songbooks are all sold (requires_grant) takes a grant on one of them,
-- or a managing role; The Big Songbook's: everyone (everybody belongs to
-- it).
CREATE FUNCTION song_visible(song bigint, person bigint) RETURNS boolean
LANGUAGE sql STABLE AS $$
  SELECT coalesce(person > 0 AND (
    s.public
    OR s.owner_person_id = person
    OR (s.owner_band_id IS NOT NULL AND EXISTS (
          SELECT 1 FROM band_members bm
          WHERE bm.band_id = s.owner_band_id AND bm.person_id = person AND upper_inf(bm.validity)))
    OR (s.owner_community_id IS NOT NULL AND (
          EXISTS (SELECT 1 FROM communities c WHERE c.id = s.owner_community_id AND c.kind = 'global')
          OR (EXISTS (SELECT 1 FROM community_members cm
                      WHERE cm.community_id = s.owner_community_id AND cm.person_id = person AND upper_inf(cm.validity))
              AND (NOT EXISTS (SELECT 1 FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
                               WHERE se.variant_id = s.id AND sb.owner_community_id = s.owner_community_id AND sb.requires_grant)
                   OR EXISTS (SELECT 1 FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
                              WHERE se.variant_id = s.id AND sb.owner_community_id = s.owner_community_id AND NOT sb.requires_grant)
                   OR EXISTS (SELECT 1 FROM community_members cm
                              JOIN community_role_privileges crp ON crp.role_id = cm.role_id
                              WHERE cm.community_id = s.owner_community_id AND cm.person_id = person AND upper_inf(cm.validity)
                                AND crp.privilege IN ('manage_members', 'manage_events', 'manage_submissions', 'manage_settings', 'manage_files'))
                   OR EXISTS (SELECT 1 FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
                              JOIN songbook_access_grants g ON g.songbook_id = sb.id AND g.person_id = person
                              WHERE se.variant_id = s.id AND sb.owner_community_id = s.owner_community_id AND sb.requires_grant
                                AND (g.expires_at IS NULL OR g.expires_at > now()))))))
  ), false)
  FROM songs s WHERE s.id = song
$$;

-- song_closed says whether song is in a closed songbook of its owner's:
-- nothing of it goes out (no link, copy, version anywhere else), and it
-- can't be public.
CREATE FUNCTION song_closed(song bigint) RETURNS boolean
LANGUAGE sql STABLE AS $$
  SELECT EXISTS (
    SELECT 1 FROM songs s
    JOIN songbook_entries se ON se.variant_id = s.id
    JOIN songbooks sb ON sb.id = se.songbook_id AND sb.closed
    WHERE s.id = song
      AND (sb.owner_person_id = s.owner_person_id OR sb.owner_band_id = s.owner_band_id
           OR sb.owner_community_id = s.owner_community_id))
$$;
