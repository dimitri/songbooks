-- Setlist scope + kind (phase 3 of the data-model rewrite): a setlist can
-- belong to a project (book), exactly as before, OR to a person's
-- personal songbook, OR to a band's repertoire — picking its entries
-- from that owner's own catalog by variant_id rather than from one
-- project's book/stem song list. scope defaults to 'project' so every
-- existing row (and every existing book/stem-addressed route/API call)
-- keeps working completely unchanged: those rows keep project_id/stem
-- populated exactly as they are today.
--
-- A person/band-scoped setlist has project_id NULL and stem '' — it's
-- addressed by its own bigserial id instead (new /setlists/{id}/...
-- routes), never by book/stem. Entries for such a setlist reference a
-- variant_id directly; see internal/setlist's extended "- variant:N"
-- entry line (added alongside the existing "- book/stem" line, same
-- body-text column, no new entries table — see that package's comment
-- for why).
CREATE TYPE setlist_scope AS ENUM ('project', 'person', 'band');
CREATE TYPE setlist_kind AS ENUM ('performance', 'themed', 'practice');

ALTER TABLE setlists ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE setlists ALTER COLUMN stem SET DEFAULT '';

ALTER TABLE setlists
  ADD COLUMN scope           setlist_scope NOT NULL DEFAULT 'project',
  ADD COLUMN owner_person_id bigint REFERENCES people(id),
  ADD COLUMN owner_band_id   bigint REFERENCES bands(id),
  ADD COLUMN kind            setlist_kind NOT NULL DEFAULT 'performance',
  ADD COLUMN reusable        boolean NOT NULL DEFAULT true,
  ADD COLUMN notes           text NOT NULL DEFAULT '';

ALTER TABLE setlists ADD CONSTRAINT setlists_scope_owner_check CHECK (
  (scope = 'project' AND project_id IS NOT NULL AND owner_person_id IS NULL AND owner_band_id IS NULL) OR
  (scope = 'person'  AND project_id IS NULL     AND owner_person_id IS NOT NULL AND owner_band_id IS NULL) OR
  (scope = 'band'    AND project_id IS NULL     AND owner_band_id IS NOT NULL AND owner_person_id IS NULL)
);

CREATE INDEX setlists_owner_person_idx ON setlists (owner_person_id) WHERE owner_person_id IS NOT NULL;
CREATE INDEX setlists_owner_band_idx ON setlists (owner_band_id) WHERE owner_band_id IS NOT NULL;

-- Performance setlists are meant to be reused across many actual gigs
-- (different dates/venues) rather than cloned per gig — a performance
-- setlist can have many gig rows attached. Themed/practice setlists
-- don't use this table at all (not forced on them): a themed songbook or
-- a practice-prep list isn't tied to calendar dates the same way.
CREATE TABLE setlist_gigs (
  id         bigserial PRIMARY KEY,
  setlist_id bigint NOT NULL REFERENCES setlists(id) ON DELETE CASCADE,
  date       date,
  venue      text NOT NULL DEFAULT '',
  notes      text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX setlist_gigs_setlist_idx ON setlist_gigs (setlist_id);
