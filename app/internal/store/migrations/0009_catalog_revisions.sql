-- Personal catalog: "this variant is in my personal songbook" — a
-- pointer from a person to a songs (variant) row, not a copy. A person
-- can point at any global variant, or at one of their own forks.
CREATE TABLE catalog_entries (
  person_id  bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  variant_id bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  added_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (person_id, variant_id)
);

-- Free-text, unthreaded notes a viewer leaves on a specific variant (the
-- work's "all versions" page) — NOT an edit to the variant's own content.
-- Structured per-section tagging is a later phase; this is just a simple
-- note list.
CREATE TABLE variant_annotations (
  id         bigserial PRIMARY KEY,
  variant_id bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  person_id  bigint NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  body       text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX variant_annotations_variant_idx ON variant_annotations (variant_id, created_at);

-- Generic revision/undo mechanism, superseding the narrow trigger-based
-- song_revisions (songs.body only). entity_type is a free-form tag
-- ('variant' now; 'setlist', band/project-membership etc. in later
-- phases — see TODOs at those call sites once they exist), entity_id is
-- that row's id cast to text so this one table works across entity types
-- with different id domains. snapshot is a JSON capture of the entity's
-- editable fields, captured by application code (internal/store/revisions.go's
-- RecordRevision) rather than a trigger, specifically so it generalizes to
-- tables a trigger can't easily compose a "whole struct as JSON" snapshot
-- from.
--
-- Convention (load-bearing, so stated plainly): a revisions row records
-- what the entity looked like *immediately before* the edit that caused
-- the row to be written — i.e. revision N is the pre-edit snapshot for
-- edit N. Reverting to revision N restores snapshot N, undoing edit N (and
-- anything after it) back to that point. This mirrors the old
-- song_revisions trigger's BEFORE UPDATE semantics (which archived
-- OLD.body before the new value landed), just generalized.
--
-- Anyone with edit rights on the entity may revert ANY revision, not only
-- their own (see store.RevertRevision) — deliberate, per the phase-1
-- brief, to support guest-editable shared material in later phases.
CREATE TABLE revisions (
  id              bigserial PRIMARY KEY,
  entity_type     text NOT NULL,
  entity_id       text NOT NULL,
  actor_person_id bigint REFERENCES people(id),
  snapshot        jsonb NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX revisions_entity_idx ON revisions (entity_type, entity_id, created_at);

-- Preserve any existing song_revisions history (a fresh/empty dev DB has
-- none — this is a no-op there) before dropping the old trigger-based
-- table.
INSERT INTO revisions (entity_type, entity_id, actor_person_id, snapshot, created_at)
SELECT 'variant', sr.song_id::text, sr.edited_by, jsonb_build_object('body', sr.body), sr.edited_at
FROM song_revisions sr;

DROP TRIGGER IF EXISTS songs_before_update ON songs;
DROP FUNCTION IF EXISTS songs_archive_revision();
DROP TABLE song_revisions;
