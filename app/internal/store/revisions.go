package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/setlist"
)

// RecordRevision snapshots entity's current editable-fields state into the
// generic revisions table, inside the same transaction tx that's about to
// write the entity's new state. Callers must pass the PRE-edit snapshot
// (see the convention documented on the revisions table in migration
// 0009) — call this immediately before issuing the UPDATE, not after.
//
// entityType is a free-form tag ('variant' today; 'setlist', band/project
// membership etc. in later phases — this is the extension point those
// phases hook into, alongside their own call to this same function right
// before their mutating write). entityID is the row's id, stringified so
// one table serves ids of any underlying type.
//
// actorPersonID is nil when the edit has no identifiable actor (e.g. a
// filesystem import run) — revisions.actor_person_id is nullable for
// exactly that case.
func (st *Store) RecordRevision(ctx context.Context, tx pgx.Tx, entityType, entityID string, actorPersonID *int64, snapshot any) error {
	_, err := recordRevision(ctx, tx, entityType, entityID, actorPersonID, snapshot, "")
	return err
}

// recordRevision is RecordRevision with the edit's one-line summary
// (migration 0125; "" for none), returning the revision's id.
func recordRevision(ctx context.Context, tx pgx.Tx, entityType, entityID string, actorPersonID *int64, snapshot any, summary string) (int64, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return 0, fmt.Errorf("marshaling %s %s snapshot: %w", entityType, entityID, err)
	}
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO revisions (entity_type, entity_id, actor_person_id, snapshot, summary)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		entityType, entityID, actorPersonID, data, summary).Scan(&id)
	return id, err
}

// VariantSnapshot is the editable-fields shape recorded for entity_type
// 'variant' — everything SaveSong/SaveRaw/RedoSong can change in one call.
type VariantSnapshot struct {
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	ReferenceURL string `json:"reference_url"`
	Body         string `json:"body"`
	// VersionName: a catalogue song's reference label before a change of
	// it (SetReferenceLabel); nil in the snapshots of other edits, whose
	// revert leaves it as it is.
	VersionName *string `json:"version_name,omitempty"`
}

// Revision is one row of an entity's revision history, as the API/history
// view needs it: who, when, and a naive diff summary against the entity's
// current state (good enough for "N lines changed" — no real diff algorithm).
type Revision struct {
	ID           int64
	EntityType   string
	EntityID     string
	ActorID      *int64
	ActorName    string // "" if ActorID is nil or the person no longer exists
	Summary      string // the edit's one-line summary, "" for none
	Snapshot     json.RawMessage
	CreatedAt    time.Time
	LinesChanged int // vs. the entity's state at the time ListRevisions was called
}

// ListRevisions returns entity's revision history, newest first, each with
// a naive line-count diff against the entity's CURRENT body (for
// entity_type 'variant'; other entity types just get 0, since they have no
// single "body" field this naive measure applies to — refine per entity
// type as later phases need it).
func (st *Store) ListRevisions(ctx context.Context, entityType, entityID string) ([]Revision, error) {
	var currentBody string
	if entityType == "variant" {
		id, err := strconv.ParseInt(entityID, 10, 64)
		if err == nil {
			_ = st.db.QueryRow(ctx, `SELECT body FROM songs WHERE id = $1`, id).Scan(&currentBody)
		}
	}

	rows, err := st.db.Query(ctx, `
		SELECT r.id, r.entity_type, r.entity_id, r.actor_person_id, coalesce(p.display_name, ''), r.summary, r.snapshot, r.created_at
		FROM revisions r
		LEFT JOIN people p ON p.id = r.actor_person_id
		WHERE r.entity_type = $1 AND r.entity_id = $2
		ORDER BY r.created_at DESC, r.id DESC`, entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Revision
	for rows.Next() {
		var rv Revision
		if err := rows.Scan(&rv.ID, &rv.EntityType, &rv.EntityID, &rv.ActorID, &rv.ActorName, &rv.Summary, &rv.Snapshot, &rv.CreatedAt); err != nil {
			return nil, err
		}
		if entityType == "variant" {
			var snap VariantSnapshot
			if json.Unmarshal(rv.Snapshot, &snap) == nil {
				rv.LinesChanged = lineDiffCount(snap.Body, currentBody)
			}
		}
		out = append(out, rv)
	}
	return out, rows.Err()
}

// lineDiffCount is a deliberately naive "how different are these two
// texts" measure: the number of line positions where the two texts
// disagree, plus the length difference in lines. Good enough for a
// history list's "~N lines changed" hint — not a real diff algorithm, and
// doesn't try to detect insertions/deletions vs. a line shift.
func lineDiffCount(a, b string) int {
	al := splitLines(a)
	bl := splitLines(b)
	n := len(al)
	if len(bl) > n {
		n = len(bl)
	}
	count := 0
	for i := 0; i < n; i++ {
		var la, lb string
		if i < len(al) {
			la = al[i]
		}
		if i < len(bl) {
			lb = bl[i]
		}
		if la != lb {
			count++
		}
	}
	return count
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// RevisionEntity returns the entity_type/entity_id a revision belongs to
// — used by the HTTP layer to check edit rights on that entity before
// allowing a revert.
func (st *Store) RevisionEntity(ctx context.Context, revisionID int64) (entityType, entityID string, err error) {
	err = st.db.QueryRow(ctx, `SELECT entity_type, entity_id FROM revisions WHERE id = $1`, revisionID).
		Scan(&entityType, &entityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("no such revision %d: %w", revisionID, ErrNotFound)
	}
	return entityType, entityID, err
}

// PeekRevisionBandID returns the band_id carried in a revision's own
// snapshot — valid for entity_type 'band_member', 'band_catalog_entry'
// and 'setlist' (all three snapshot shapes carry a "band_id" key — for
// 'setlist' it's null unless scope='band'), without needing the entity's
// CURRENT row to still exist (which it may
// not: reverting an "add" deletes/soft-removes it). Used by
// handleRevertRevision to check "is the actor a current member of this
// band" before allowing a band-scoped revert, directly off the revision
// being reverted rather than off a row that might be gone.
func (st *Store) PeekRevisionBandID(ctx context.Context, revisionID int64) (int64, error) {
	var bandID int64
	err := st.db.QueryRow(ctx, `SELECT (snapshot->>'band_id')::bigint FROM revisions WHERE id = $1`, revisionID).Scan(&bandID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("no such revision %d: %w", revisionID, ErrNotFound)
	}
	return bandID, err
}

// RevertRevision restores the entity a revision belongs to back to that
// revision's snapshot, then records a fresh revision capturing the state
// the entity was in right before the revert — so the revert is itself
// undoable, never a destructive skip of the trail.
//
// Known-permissive placeholder for this phase: the caller is responsible
// for checking "actorID has edit rights on the entity" before calling this
// — see httpapi's handleRevertRevision, which currently allows ANY
// logged-in person to revert ANY global variant's edits (no band/role
// concept is attached to global variants yet) and only the owner to
// revert their own user-visibility variants. Later phases scope this down
// via band roles.
func (st *Store) RevertRevision(ctx context.Context, revisionID int64, actorPersonID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var entityType, entityID string
	var snapshotData []byte
	err = tx.QueryRow(ctx, `SELECT entity_type, entity_id, snapshot FROM revisions WHERE id = $1`, revisionID).
		Scan(&entityType, &entityID, &snapshotData)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no such revision %d: %w", revisionID, ErrNotFound)
	}
	if err != nil {
		return err
	}

	switch entityType {
	case "variant":
		var snap VariantSnapshot
		if err := json.Unmarshal(snapshotData, &snap); err != nil {
			return fmt.Errorf("decoding revision %d snapshot: %w", revisionID, err)
		}
		id, err := strconv.ParseInt(entityID, 10, 64)
		if err != nil {
			return fmt.Errorf("revision %d has a non-numeric variant id %q: %w", revisionID, entityID, err)
		}

		var curTitle, curArtist, curRef, curBody, curName string
		err = tx.QueryRow(ctx, `
			SELECT title, artist, reference_url, body, version_name FROM songs WHERE id = $1 FOR UPDATE`, id).
			Scan(&curTitle, &curArtist, &curRef, &curBody, &curName)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no such variant %d: %w", id, ErrNotFound)
		}
		if err != nil {
			return err
		}

		actor := &actorPersonID
		// The catalogue's edits it undoes are reverted (patrol, trust).
		if err := markEditsReverted(ctx, tx, revisionID, actorPersonID); err != nil {
			return err
		}
		undo := VariantSnapshot{Title: curTitle, Artist: curArtist, ReferenceURL: curRef, Body: curBody}
		if snap.VersionName != nil {
			undo.VersionName = &curName
		}
		if err := st.RecordRevision(ctx, tx, "variant", entityID, actor, undo); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE songs SET title = $1, artist = $2, reference_url = $3, body = $4, version = version + 1,
			                 version_name = coalesce($6, version_name)
			WHERE id = $5`,
			snap.Title, snap.Artist, snap.ReferenceURL, snap.Body, id, snap.VersionName); err != nil {
			return err
		}
		if _, err := shiftLeadSync(ctx, tx, id, curBody, snap.Body); err != nil {
			return err
		}
		if _, err := keepEntryKeys(ctx, tx, id, curBody, snap.Body); err != nil {
			return err
		}

	case "band_member":
		var snap BandMemberSnapshot
		if err := json.Unmarshal(snapshotData, &snap); err != nil {
			return fmt.Errorf("decoding revision %d snapshot: %w", revisionID, err)
		}
		id, err := strconv.ParseInt(entityID, 10, 64)
		if err != nil {
			return fmt.Errorf("revision %d has a non-numeric band_member id %q: %w", revisionID, entityID, err)
		}

		var curBandID, curPersonID int64
		var curStatus string
		var curRoleID int64
		var curJoinedAt time.Time
		var curLeftAt *time.Time
		var curInvitedBy *int64
		err = tx.QueryRow(ctx, `
			SELECT band_id, person_id, status::text, role_id, lower(validity),
			       CASE WHEN upper_inf(validity) THEN NULL ELSE upper(validity) END, invited_by_person_id
			FROM band_members WHERE id = $1 FOR UPDATE`, id).
			Scan(&curBandID, &curPersonID, &curStatus, &curRoleID, &curJoinedAt, &curLeftAt, &curInvitedBy)
		rowExists := !errors.Is(err, pgx.ErrNoRows)
		if err != nil && rowExists {
			return err
		}

		actor := &actorPersonID
		if rowExists {
			if err := st.RecordRevision(ctx, tx, "band_member", entityID, actor, BandMemberSnapshot{
				Exists: true, BandID: curBandID, PersonID: curPersonID, Status: curStatus, RoleID: curRoleID,
				JoinedAt: curJoinedAt, LeftAt: curLeftAt, InvitedByPersonID: curInvitedBy,
			}); err != nil {
				return err
			}
		} else {
			if err := st.RecordRevision(ctx, tx, "band_member", entityID, actor, BandMemberSnapshot{Exists: false}); err != nil {
				return err
			}
		}

		if !snap.Exists {
			// Undoes an Add: the row didn't exist before that mutation, so
			// reverting to this snapshot removes it. A hard delete (rather
			// than the soft left_at-only removal RemoveBandMember uses) is
			// deliberate here: this specific row is only ever reachable by
			// this one revert path undoing its own creation, so there's no
			// "current membership" row left dangling to confuse with a real
			// departure.
			if _, err := tx.Exec(ctx, `DELETE FROM band_members WHERE id = $1`, id); err != nil {
				return err
			}
		} else {
			if !rowExists {
				return fmt.Errorf("band membership row %d no longer exists, can't restore it: %w", id, ErrNotFound)
			}
			// The role it had, if the band still has it; else the
			// built-in role of its status (migration 0130's trigger).
			if _, err := tx.Exec(ctx, `
				UPDATE band_members SET status = $1::text::band_member_status, validity = tstzrange(lower(validity), $2::timestamptz),
				       role_id = coalesce((SELECT r.id FROM band_roles r WHERE r.id = $4 AND r.band_id = band_members.band_id),
				                          (SELECT r.id FROM band_roles r WHERE r.band_id = band_members.band_id AND r.builtin_key = $1::text))
				WHERE id = $3`,
				snap.Status, snap.LeftAt, id, snap.RoleID); err != nil {
				return err
			}
		}

	case "setlist":
		var snap SetlistSnapshot
		if err := json.Unmarshal(snapshotData, &snap); err != nil {
			return fmt.Errorf("decoding revision %d snapshot: %w", revisionID, err)
		}
		id, err := strconv.ParseInt(entityID, 10, 64)
		if err != nil {
			return fmt.Errorf("revision %d has a non-numeric setlist id %q: %w", revisionID, entityID, err)
		}

		cur, _, rowExists, err := setlistSnapshot(ctx, tx, id)
		if err != nil {
			return err
		}

		actor := &actorPersonID
		if err := st.RecordRevision(ctx, tx, "setlist", entityID, actor, cur); err != nil {
			return err
		}

		if !snap.Exists {
			// Undoes a Create — see the band_member/band_catalog_entry
			// cases above for why a hard delete is the right undo for a
			// creation.
			if _, err := tx.Exec(ctx, `DELETE FROM setlists WHERE id = $1`, id); err != nil {
				return err
			}
		} else {
			if !rowExists {
				return fmt.Errorf("setlist %d no longer exists, can't restore it: %w", id, ErrNotFound)
			}
			// The snapshot's text form back into rows (a revision recorded
			// before migration 0115 is the body as it was then).
			if err := writeSetlistItems(ctx, tx, id, setlist.Parse(snap.Body)); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE setlists SET title = $1, notes = $2, version = version + 1
				WHERE id = $3`,
				snap.Title, snap.Notes, id); err != nil {
				return err
			}
		}

	default:
		return fmt.Errorf("revert not implemented for entity_type %q", entityType)
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}
