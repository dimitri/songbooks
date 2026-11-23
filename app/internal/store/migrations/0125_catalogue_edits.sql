-- The catalogue: open edits (docs/ownership-model.md, "The catalogue:
-- open edits"). Anyone signed in edits The Big Songbook's songs; nothing
-- is lost (every edit a revision, with a one-line summary, revertable);
-- the reference recording is the anchor: an edit that keeps or improves
-- how the chart follows it is applied at once, one that makes it worse,
-- or touches the structure, the lead-in or the Guitar Pro sync, waits
-- for review (store/catalogue_edits.go).

-- A revision's one-line summary, asked at save (optional; '' for none).
ALTER TABLE revisions ADD COLUMN summary text NOT NULL DEFAULT '';

-- A person kept from editing the catalogue by its staff (NULL: not).
ALTER TABLE people ADD COLUMN catalogue_banned_at timestamptz;

-- catalogue_edits: every catalogue edit by someone who isn't its staff,
-- applied at once ('applied', revision_id the revision it made) or held
-- for review ('pending': the song stays as it was; 'approved' makes it
-- current, 'rejected' with a note to its author; 'superseded' when its
-- author saved another edit of the same song before it was decided).
-- base_* is the song as the author edited it (a stale base is merged
-- with what changed since, songdiff.Merge, on approval). reasons: why it
-- waits ('fit', 'structure', 'lead', 'gp', 'key', 'reference',
-- 'newcomer'). fit_before/fit_after: songimport.RecordingFit's verdicts
-- ('' when the recording has no analysis), their reasons in the details.
-- Accepted edits ('applied', 'approved') count towards trust.
CREATE TABLE catalogue_edits (
  id                bigserial PRIMARY KEY,
  song_id           bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  author_id         bigint REFERENCES people(id) ON DELETE SET NULL,
  status            text NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('applied', 'pending', 'approved', 'rejected', 'superseded')),
  base_version      integer NOT NULL,
  base_body         text NOT NULL,
  title             text NOT NULL,
  artist            text NOT NULL,
  reference_url     text NOT NULL DEFAULT '',
  body              text NOT NULL,
  summary           text NOT NULL DEFAULT '',
  reasons           text[] NOT NULL DEFAULT '{}',
  fit_before        text NOT NULL DEFAULT '',
  fit_after         text NOT NULL DEFAULT '',
  fit_before_detail jsonb,
  fit_after_detail  jsonb,
  revision_id       bigint REFERENCES revisions(id) ON DELETE SET NULL,
  decided_by        bigint REFERENCES people(id) ON DELETE SET NULL,
  decided_at        timestamptz,
  note              text NOT NULL DEFAULT '',
  created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX catalogue_edits_song_idx ON catalogue_edits (song_id, created_at);
CREATE INDEX catalogue_edits_author_idx ON catalogue_edits (author_id, status);
CREATE INDEX catalogue_edits_pending_idx ON catalogue_edits (created_at) WHERE status = 'pending';
-- One edit waiting per author and song: a new one supersedes it.
CREATE UNIQUE INDEX catalogue_edits_one_pending ON catalogue_edits (song_id, author_id) WHERE status = 'pending';

-- catalogue_reports: "Report" on a catalogue song (a reason), for the
-- review queue until its staff or a trusted editor marks it handled.
CREATE TABLE catalogue_reports (
  id          bigserial PRIMARY KEY,
  song_id     bigint NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
  person_id   bigint REFERENCES people(id) ON DELETE SET NULL,
  reason      text NOT NULL,
  handled_by  bigint REFERENCES people(id) ON DELETE SET NULL,
  handled_at  timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX catalogue_reports_open_idx ON catalogue_reports (created_at) WHERE handled_at IS NULL;
