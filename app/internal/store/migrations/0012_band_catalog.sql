-- Band catalog: "this variant is in band X's repertoire" — same pointer
-- mechanism as catalog_entries (migration 0009) but band-scoped. Unlike
-- catalog_entries, this table has its own bigserial id rather than a
-- composite (band_id, variant_id) primary key, so add/remove mutations
-- can be addressed by the generic revisions table the same way variant
-- edits and band_member mutations are (see internal/store/revisions.go) —
-- a deliberate, small deviation from catalog_entries' shape, needed to
-- make band-catalog add/remove revertible per the phase-2 brief.
CREATE TABLE band_catalog_entries (
  id                 bigserial PRIMARY KEY,
  band_id            bigint NOT NULL REFERENCES bands(id) ON DELETE CASCADE,
  variant_id         bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  added_at           timestamptz NOT NULL DEFAULT now(),
  added_by_person_id bigint REFERENCES people(id),
  UNIQUE (band_id, variant_id)
);
