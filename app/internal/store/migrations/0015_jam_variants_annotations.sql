-- owner_jam_event_id: parallel to owner_person_id (migration 0008) and
-- owner_band_id (migration 0011), for visibility='jam' variants a jam
-- participant creates from scratch or forks for that one jam event only
-- ("it's like a band per song for the jam night only" — jam_events plays
-- the same ephemeral-group role here that bands play for visibility=
-- 'band'). A jam-scoped variant is deliberately excluded from
-- ListCatalog/ListVariantsForWork (see catalog.go) — it's only reachable
-- via the jam's own song list, same spirit as a band's material not
-- leaking into another band's view.
ALTER TABLE songs
  ADD COLUMN owner_jam_event_id bigint REFERENCES jam_events(id);
CREATE INDEX songs_owner_jam_event_idx ON songs (owner_jam_event_id) WHERE owner_jam_event_id IS NOT NULL;

-- Lightweight, per-section notes ("solo: sax") — NOT an edit to the
-- variant's own body, just a tag attached to one section (section_index
-- lines up with buildSections'/song.Song's zero-based section ordering —
-- see httpapi.buildSections). Usable both for a real jam annotation
-- (jam_event_id + jam_profile_id set, since an unlogged jam participant
-- has no person_id — see jam_profiles) and, as a smaller side-benefit,
-- for a logged-in person annotating their own/band's variant outside any
-- jam (jam_event_id/jam_profile_id both null, person_id set). The check
-- constraint only requires SOME identified annotator, not jam-only.
CREATE TABLE section_annotations (
  id             bigserial PRIMARY KEY,
  variant_id     bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  section_index  integer NOT NULL,
  jam_event_id   bigint REFERENCES jam_events(id) ON DELETE CASCADE,
  person_id      bigint REFERENCES people(id) ON DELETE CASCADE,
  jam_profile_id bigint REFERENCES jam_profiles(id) ON DELETE CASCADE,
  body           text NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT section_annotations_annotator_check CHECK (person_id IS NOT NULL OR jam_profile_id IS NOT NULL)
);
CREATE INDEX section_annotations_variant_idx ON section_annotations (variant_id, section_index, created_at);
