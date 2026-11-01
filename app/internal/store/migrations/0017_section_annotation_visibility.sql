-- "Build annotation editor (take notes) with personal or shared notes."
-- A visibility dimension on section_annotations: 'private' (default) is
-- visible only to its own author (person_id or jam_profile_id match);
-- 'shared' is visible to anyone who can already view the variant. No
-- separate "can view this variant" helper exists yet (see the brief) —
-- gating is done directly in the list queries (see
-- store.ListSectionAnnotations/ListAllSectionAnnotations), not here.
ALTER TABLE section_annotations
  ADD COLUMN visibility text NOT NULL DEFAULT 'private'
    CHECK (visibility IN ('private', 'shared'));
