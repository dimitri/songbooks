package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/songdiff"
)

// The original's changes offered to its versions (docs/ownership-model.md
// §5, migration 0117). A version records the original's chart as it was
// when it was made or last synced (base_body). When the original's chart
// changes, the version's page shows what changed since and its editors
// take the change — part by part where it applies cleanly (songdiff.Plan,
// songdiff.Merge) — or keep theirs; either way the version is synced
// with the original as it is then.
//
// Only a change to the chart counts: not whitespace, comments, or the
// song's metadata alone (title, artist, reference, cover —
// songdiff.ChartChanged). No notification is sent (2026-11-24): the
// change is listed on the home page of whoever may take it, with the
// offer (song_news.go), until they mark the list seen.

// NotificationVersionChanged is the kind of "the original changed",
// sent until 2026-11-24: the ones still unread are marked read when
// the version takes or keeps the change.
const NotificationVersionChanged = "version_original_changed"

// VersionChangedLink is where a version's editors see what changed.
func VersionChangedLink(versionID int64) string {
	return fmt.Sprintf("/song/%d#original-changed", versionID)
}

// OriginalChange is what the original of a version did since the version
// last synced with it, for the version's page.
type OriginalChange struct {
	VersionID  int64
	OriginalID int64
	Title      string // the original's
	Artist     string
	OwnerKind  string // the original's kind: 'global', 'user', 'band', 'community'
	OwnerName  string
	// OriginalVersion is the original's version number: taking or keeping
	// answers for that one (ErrConflict when it changed again since).
	OriginalVersion int
	SyncedAt        *time.Time
	ChangedAt       *time.Time // the original's last save
	Changes         []songdiff.Change
	Lines           []songdiff.Line
	Parts           []songdiff.Part
	// Takeable: some part applies cleanly.
	Takeable bool
}

// OriginalChangeFor is what versionID's original changed since the
// version last synced, for personID, who may change the version (nil
// otherwise, or when there's nothing to offer: not a version, a frozen
// copy, an original personID can't open any more, or no change to its
// chart).
func (st *Store) OriginalChangeFor(ctx context.Context, personID, versionID int64) (*OriginalChange, error) {
	var origID *int64
	var base *string
	var ours string
	var syncedAt *time.Time
	err := st.db.QueryRow(ctx, `
		SELECT forked_from_id, base_body, body, base_synced_at FROM songs WHERE id = $1 AND frozen_at IS NULL`, versionID).
		Scan(&origID, &base, &ours, &syncedAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (origID == nil || base == nil)) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r, err := st.SongRightsFor(ctx, personID, versionID)
	if err != nil || !r.Edit {
		return nil, err
	}
	or, err := st.SongRightsFor(ctx, personID, *origID)
	if err != nil || !or.See {
		return nil, err
	}
	c := &OriginalChange{VersionID: versionID, OriginalID: *origID, SyncedAt: syncedAt}
	var theirs string
	if err := st.db.QueryRow(ctx, `
		SELECT title, artist, body, version,
		       (SELECT max(created_at) FROM revisions WHERE entity_type = 'variant' AND entity_id = $1::bigint::text)
		FROM songs WHERE id = $1`, *origID).
		Scan(&c.Title, &c.Artist, &theirs, &c.OriginalVersion, &c.ChangedAt); err != nil {
		return nil, err
	}
	if !songdiff.ChartChanged(*base, theirs) {
		return nil, nil
	}
	c.Parts = songdiff.Plan(*base, ours, theirs)
	if len(c.Parts) == 0 {
		// The version has it already (made the same change itself):
		// nothing to take, nothing to tell any more.
		return nil, nil
	}
	for _, p := range c.Parts {
		c.Takeable = c.Takeable || !p.Conflict
	}
	c.Changes = songdiff.Describe(*base, theirs)
	c.Lines = songdiff.Lines(*base, theirs)
	if o, err := st.VariantOwnerOf(ctx, *origID); err == nil {
		c.OwnerKind, c.OwnerName = o.Kind, o.Name
	}
	return c, nil
}

// SyncWithOriginal answers the original's change for personID, who may
// change versionID: take is the parts to take (songdiff.Part.Key; none:
// "Keep ours"), a conflicting part never taken. Either way the version is
// synced with the original as it is at originalVersion (ErrConflict when
// the original changed again since: the page is stale). Taking records a
// revision of the version, as any edit does. Returns whether the
// version's chart changed.
func (st *Store) SyncWithOriginal(ctx context.Context, personID, versionID int64, originalVersion int, take []string) (bool, error) {
	r, err := st.SongRightsFor(ctx, personID, versionID)
	if err != nil {
		return false, err
	}
	if !r.Edit {
		return false, fmt.Errorf("person %d may not change song %d: %w", personID, versionID, ErrForbidden)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var origID *int64
	var base *string
	var title, artist, ref, ours string
	err = tx.QueryRow(ctx, `
		SELECT forked_from_id, base_body, title, artist, reference_url, body FROM songs
		WHERE id = $1 AND frozen_at IS NULL FOR UPDATE`, versionID).
		Scan(&origID, &base, &title, &artist, &ref, &ours)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (origID == nil || base == nil)) {
		return false, fmt.Errorf("song %d follows no original: %w", versionID, ErrNotFound)
	}
	if err != nil {
		return false, err
	}
	var sees bool
	var theirs string
	var theirVersion int
	err = tx.QueryRow(ctx, `SELECT song_visible(id, $2), body, version FROM songs WHERE id = $1 FOR SHARE`, *origID, personID).
		Scan(&sees, &theirs, &theirVersion)
	if err != nil {
		return false, err
	}
	if !sees {
		return false, fmt.Errorf("person %d can't open song %d: %w", personID, *origID, ErrForbidden)
	}
	if theirVersion != originalVersion {
		return false, ErrConflict
	}
	want := map[string]bool{}
	for _, k := range take {
		want[k] = true
	}
	merged := ours
	if len(want) > 0 {
		merged = songdiff.Merge(*base, ours, theirs, func(k string) bool { return want[k] })
	}
	changed := merged != ours
	if changed {
		actor := &personID
		if err := st.RecordRevision(ctx, tx, "variant", strconv.FormatInt(versionID, 10), actor, VariantSnapshot{
			Title: title, Artist: artist, ReferenceURL: ref, Body: ours,
		}); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `UPDATE songs SET body = $2, version = version + 1 WHERE id = $1`, versionID, merged); err != nil {
			return false, err
		}
		if _, err := shiftLeadSync(ctx, tx, versionID, ours, merged); err != nil {
			return false, err
		}
		if _, err := keepEntryKeys(ctx, tx, versionID, ours, merged); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE songs SET base_body = $2, base_version = $3, base_synced_at = now() WHERE id = $1`,
		versionID, theirs, theirVersion); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE notifications SET read_at = now() WHERE kind = $1 AND link = $2 AND read_at IS NULL`,
		NotificationVersionChanged, VersionChangedLink(versionID)); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return changed, nil
}
