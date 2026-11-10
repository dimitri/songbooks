-- Collapses band_members'/community_members' joined_at/left_at timestamp
-- pair into a single validity tstzrange column per row — still one row
-- per membership stint (NOT a multirange collapse: that would destroy
-- the per-row identity revisions.go's undo and songexport/songimport's
-- per-stint invited_by_person_id both depend on). "Current membership"
-- becomes upper_inf(validity) instead of left_at IS NULL; a plain
-- btree partial-unique index on that boolean predicate still needs no
-- GiST/exclusion constraint or btree_gist extension.
ALTER TABLE band_members ADD COLUMN validity tstzrange;
UPDATE band_members SET validity = tstzrange(joined_at, left_at);
ALTER TABLE band_members ALTER COLUMN validity SET NOT NULL;
ALTER TABLE band_members ALTER COLUMN validity SET DEFAULT tstzrange(now(), null);

DROP INDEX band_members_current_unique;
CREATE UNIQUE INDEX band_members_current_unique ON band_members (band_id, person_id) WHERE upper_inf(validity);
DROP INDEX band_members_band_idx;
DROP INDEX band_members_person_idx;
CREATE INDEX band_members_band_idx ON band_members (band_id);
CREATE INDEX band_members_person_idx ON band_members (person_id);

ALTER TABLE band_members DROP COLUMN joined_at, DROP COLUMN left_at;

ALTER TABLE community_members ADD COLUMN validity tstzrange;
UPDATE community_members SET validity = tstzrange(joined_at, left_at);
ALTER TABLE community_members ALTER COLUMN validity SET NOT NULL;
ALTER TABLE community_members ALTER COLUMN validity SET DEFAULT tstzrange(now(), null);

DROP INDEX community_members_current_unique;
CREATE UNIQUE INDEX community_members_current_unique ON community_members (community_id, person_id) WHERE upper_inf(validity);
DROP INDEX community_members_community_idx;
DROP INDEX community_members_person_idx;
CREATE INDEX community_members_community_idx ON community_members (community_id);
CREATE INDEX community_members_person_idx ON community_members (person_id);

ALTER TABLE community_members DROP COLUMN joined_at, DROP COLUMN left_at;
