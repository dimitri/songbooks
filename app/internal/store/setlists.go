package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/setlist"
)

// --- setlist scope (person/band-owned setlists, addressed by
// id rather than book/stem) and gigs -----------------------------------

// SetlistMeta is a setlist's scope/ownership metadata — everything
// about a setlist other than its parsed entries, as the id-addressed
// routes (practice/listening, and person/band CRUD) need it. Book is ""
// for a person/band-scoped setlist (project_id is NULL for those).
//
// kind/reusable (performance/themed/practice) were removed per the user's
// own call: they were only explanatory scaffolding for scope
// (project/person/band), which is the distinction that actually matters
// and is the only one kept — see migration 0016.
type SetlistMeta struct {
	ID            int64
	Scope         string // "person", "band", or "school" (community)
	Notes         string
	Title         string
	OwnerPersonID *int64
	OwnerBandID   *int64
	OwnerSchoolID *int64
	Version       int
	CreatedAt     time.Time
}

// SetlistSnapshot is the entity_type 'setlist' snapshot shape recorded by
// RecordRevision — everything SaveSetlistByID can change, plus the
// ownership fields (which never change across an edit, but are needed so
// a revert can be permission-checked, and so undoing a Create can tell
// "this row shouldn't exist" apart from "restore these field values" the
// same way BandCatalogEntrySnapshot's Exists convention does). The JSON
// keys "person_id"/"band_id" match PeekRevisionBandID's existing
// generic `snapshot->>'band_id'` lookup, reused here for setlists too.
type SetlistSnapshot struct {
	Exists        bool   `json:"exists"`
	Scope         string `json:"scope"`
	Title         string `json:"title"`
	Notes         string `json:"notes"`
	Body          string `json:"body"`
	OwnerPersonID *int64 `json:"person_id"`
	OwnerBandID   *int64 `json:"band_id"`
}

// GetSetlistMeta loads one setlist's scope/ownership metadata by id —
// works for any scope, project-scoped setlists included.
func (st *Store) GetSetlistMeta(ctx context.Context, id int64) (*SetlistMeta, error) {
	var m SetlistMeta
	err := st.db.QueryRow(ctx, `
		SELECT sl.id, sl.scope::text, sl.notes, sl.title,
		       sl.owner_person_id, sl.owner_band_id, sl.owner_school_id, sl.version, sl.created_at
		FROM setlists sl WHERE sl.id = $1`, id).
		Scan(&m.ID, &m.Scope, &m.Notes, &m.Title,
			&m.OwnerPersonID, &m.OwnerBandID, &m.OwnerSchoolID, &m.Version, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such setlist %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// LoadSetlistByID loads one setlist's parsed entries plus its metadata,
// by id — the resolution path practice/listening use, which works
// identically regardless of scope (person or band).
func (st *Store) LoadSetlistByID(ctx context.Context, id int64) (*setlist.Setlist, *SetlistMeta, string, error) {
	meta, err := st.GetSetlistMeta(ctx, id)
	if err != nil {
		return nil, nil, "", err
	}
	var body string
	if err := st.db.QueryRow(ctx, `SELECT body FROM setlists WHERE id = $1`, id).Scan(&body); err != nil {
		return nil, nil, "", err
	}
	return setlist.Parse(body), meta, strconv.Itoa(meta.Version), nil
}

// CanViewSetlistByID reports whether personID may see setlistID's
// content — person scope: the owner only; band scope: any current band
// member.
func (st *Store) CanViewSetlistByID(ctx context.Context, personID, setlistID int64) (bool, error) {
	meta, err := st.GetSetlistMeta(ctx, setlistID)
	if err != nil {
		return false, err
	}
	switch meta.Scope {
	case "person":
		return meta.OwnerPersonID != nil && *meta.OwnerPersonID == personID, nil
	case "band":
		if meta.OwnerBandID == nil {
			return false, nil
		}
		return st.IsCurrentBandMember(ctx, *meta.OwnerBandID, personID)
	case "school":
		if meta.OwnerSchoolID == nil {
			return false, nil
		}
		return isCurrentSchoolMember(ctx, st.db, *meta.OwnerSchoolID, personID, "")
	default:
		return false, nil
	}
}

// CanEditSetlistByID reports whether personID may edit setlistID —
// person scope: the owner only (no sharing — it's their personal
// setlist); band scope: any current band member (same open-
// collaboration rule as a band's songbook); school(community) scope:
// staff only (the existing 'instructor' role) — a read-only community
// member can view and use the community's setlists but not edit them,
// same split the user asked for on membership generally.
func (st *Store) CanEditSetlistByID(ctx context.Context, personID, setlistID int64) (bool, error) {
	meta, err := st.GetSetlistMeta(ctx, setlistID)
	if err != nil {
		return false, err
	}
	switch meta.Scope {
	case "person":
		return meta.OwnerPersonID != nil && *meta.OwnerPersonID == personID, nil
	case "band":
		if meta.OwnerBandID == nil {
			return false, nil
		}
		return st.IsCurrentBandMember(ctx, *meta.OwnerBandID, personID)
	case "school":
		if meta.OwnerSchoolID == nil {
			return false, nil
		}
		return isCurrentSchoolMember(ctx, st.db, *meta.OwnerSchoolID, personID, "instructor")
	default:
		return false, nil
	}
}

// CreateSetlist creates a new person- or band-scoped setlist. Exactly
// one of ownerPersonID/ownerBandID must be set, matching scope. Returns
// the new setlist's id.
func (st *Store) CreateSetlist(ctx context.Context, scope, title, notes string, ownerPersonID, ownerBandID, ownerSchoolID *int64, actorPersonID int64) (int64, error) {
	if scope != "person" && scope != "band" && scope != "school" {
		return 0, fmt.Errorf("invalid setlist scope %q for CreateSetlist", scope)
	}
	if scope == "person" && ownerPersonID == nil {
		return 0, fmt.Errorf("owner_person_id is required for a person-scoped setlist")
	}
	if scope == "band" && ownerBandID == nil {
		return 0, fmt.Errorf("owner_band_id is required for a band-scoped setlist")
	}
	if scope == "school" && ownerSchoolID == nil {
		return 0, fmt.Errorf("owner_school_id is required for a school(community)-scoped setlist")
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO setlists (title, body, scope, owner_person_id, owner_band_id, owner_school_id, notes)
		VALUES ($1, '', $2, $3, $4, $5, $6)
		RETURNING id`,
		title, scope, ownerPersonID, ownerBandID, ownerSchoolID, notes).Scan(&id); err != nil {
		return 0, err
	}

	actor := &actorPersonID
	if err := st.RecordRevision(ctx, tx, "setlist", strconv.FormatInt(id, 10), actor, SetlistSnapshot{Exists: false}); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

// SaveSetlistByID edits a setlist's title/notes fields and its entries,
// by id — the path person/band-scoped setlists are edited through
// (project-scoped setlists keep using SaveSetlist's book/stem path
// unchanged). If expectVersion is non-empty, the save is rejected with
// ErrConflict when the row's current version doesn't match.
func (st *Store) SaveSetlistByID(ctx context.Context, id int64, l *setlist.Setlist, title, notes string, expectVersion string, actorPersonID int64) (string, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var curScope, curTitle, curNotes, curBody string
	var curOwnerPerson, curOwnerBand *int64
	var curVersion int
	err = tx.QueryRow(ctx, `
		SELECT scope::text, title, notes, body, owner_person_id, owner_band_id, version
		FROM setlists WHERE id = $1 FOR UPDATE`, id).
		Scan(&curScope, &curTitle, &curNotes, &curBody, &curOwnerPerson, &curOwnerBand, &curVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no such setlist %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	if expectVersion != "" && strconv.Itoa(curVersion) != expectVersion {
		return "", ErrConflict
	}

	var actor *int64
	if actorPersonID != 0 {
		actor = &actorPersonID
	}
	if err := st.RecordRevision(ctx, tx, "setlist", strconv.FormatInt(id, 10), actor, SetlistSnapshot{
		Exists: true, Scope: curScope, Title: curTitle, Notes: curNotes,
		Body: curBody, OwnerPersonID: curOwnerPerson, OwnerBandID: curOwnerBand,
	}); err != nil {
		return "", err
	}

	body := setlist.Write(l)
	var newVersion int
	if err := tx.QueryRow(ctx, `
		UPDATE setlists SET title = $1, notes = $2, body = $3, version = version + 1
		WHERE id = $4
		RETURNING version`,
		title, notes, body, id).Scan(&newVersion); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return strconv.Itoa(newVersion), nil
}

// DeleteSetlistByID permanently deletes a setlist (and, via FK cascade,
// any setlist_gigs attached to it) — any scope.
func (st *Store) DeleteSetlistByID(ctx context.Context, id int64) error {
	tag, err := st.db.Exec(ctx, `DELETE FROM setlists WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such setlist %d: %w", id, ErrNotFound)
	}
	return nil
}

// ListSetlistsForPerson lists every person-scoped setlist personID owns,
// most recently created first.
func (st *Store) ListSetlistsForPerson(ctx context.Context, personID int64) ([]SetlistMeta, error) {
	return st.listSetlistsByOwner(ctx, `owner_person_id = $1`, personID)
}

// ListSetlistsForBand lists every band-scoped setlist bandID owns, most
// recently created first.
func (st *Store) ListSetlistsForBand(ctx context.Context, bandID int64) ([]SetlistMeta, error) {
	return st.listSetlistsByOwner(ctx, `owner_band_id = $1`, bandID)
}

// ListSetlistsForSchool lists every school(community)-scoped setlist
// schoolID owns, most recently created first.
func (st *Store) ListSetlistsForSchool(ctx context.Context, schoolID int64) ([]SetlistMeta, error) {
	return st.listSetlistsByOwner(ctx, `owner_school_id = $1`, schoolID)
}

func (st *Store) listSetlistsByOwner(ctx context.Context, whereClause string, ownerID int64) ([]SetlistMeta, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, scope::text, notes, title, owner_person_id, owner_band_id, owner_school_id, version, created_at
		FROM setlists WHERE `+whereClause+` ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SetlistMeta
	for rows.Next() {
		var m SetlistMeta
		if err := rows.Scan(&m.ID, &m.Scope, &m.Notes, &m.Title,
			&m.OwnerPersonID, &m.OwnerBandID, &m.OwnerSchoolID, &m.Version, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetlistGig is one calendar date a performance setlist was/will be
// played at. Only meaningful for kind='performance' setlists — themed
// and practice setlists never get rows here.
type SetlistGig struct {
	ID        int64
	SetlistID int64
	Date      *time.Time
	Venue     string
	Notes     string
	CreatedAt time.Time
}

// AddSetlistGig attaches a gig (date/venue/notes) to setlistID. date may
// be nil (venue/date to-be-decided).
func (st *Store) AddSetlistGig(ctx context.Context, setlistID int64, date *time.Time, venue, notes string) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO setlist_gigs (setlist_id, date, venue, notes) VALUES ($1, $2, $3, $4)
		RETURNING id`, setlistID, date, venue, notes).Scan(&id)
	return id, err
}

// ListSetlistGigs lists setlistID's gig rows, soonest/most-recent first
// (by date, nulls last, then by id).
func (st *Store) ListSetlistGigs(ctx context.Context, setlistID int64) ([]SetlistGig, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, setlist_id, date, venue, notes, created_at
		FROM setlist_gigs WHERE setlist_id = $1
		ORDER BY date NULLS LAST, id`, setlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SetlistGig
	for rows.Next() {
		var g SetlistGig
		if err := rows.Scan(&g.ID, &g.SetlistID, &g.Date, &g.Venue, &g.Notes, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RemoveSetlistGig deletes one gig row by id.
func (st *Store) RemoveSetlistGig(ctx context.Context, gigID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM setlist_gigs WHERE id = $1`, gigID)
	return err
}

// UpcomingGig is one future gig row, flattened with enough of its
// setlist's own context (title, which band if any) for a "coming up"
// card to render without a second round trip.
type UpcomingGig struct {
	ID           int64
	SetlistID    int64
	Date         time.Time
	Venue        string
	Notes        string
	SetlistTitle string
	BandName     string // "" for a personal setlist
	BandID       *int64 // nil for a personal setlist
}

// ListUpcomingGigsForPerson finds the soonest (today or later) gigs
// across every setlist personID can see — their own person-scoped
// setlists, plus every band-scoped setlist belonging to a band they're
// currently a member of. Used by the home page's "coming up" card, so
// capped at a handful rather than every future gig ever entered.
func (st *Store) ListUpcomingGigsForPerson(ctx context.Context, personID int64, limit int) ([]UpcomingGig, error) {
	rows, err := st.db.Query(ctx, `
		SELECT g.id, g.setlist_id, g.date, g.venue, g.notes,
		       sl.title, coalesce(b.name, ''), b.id
		FROM setlist_gigs g
		JOIN setlists sl ON sl.id = g.setlist_id
		LEFT JOIN bands b ON b.id = sl.owner_band_id
		WHERE g.date >= current_date
		  AND (
		    sl.owner_person_id = $1
		    OR sl.owner_band_id IN (
		      SELECT band_id FROM band_members WHERE person_id = $1 AND left_at IS NULL
		    )
		  )
		ORDER BY g.date ASC, g.id ASC
		LIMIT $2`, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UpcomingGig
	for rows.Next() {
		var g UpcomingGig
		if err := rows.Scan(&g.ID, &g.SetlistID, &g.Date, &g.Venue, &g.Notes, &g.SetlistTitle, &g.BandName, &g.BandID); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
