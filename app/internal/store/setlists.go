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

// ListSetlists returns the stems of every setlist in book.
func (st *Store) ListSetlists(book string) ([]string, error) {
	rows, err := st.db.Query(context.Background(), `
		SELECT sl.stem FROM setlists sl JOIN projects p ON p.id = sl.project_id
		WHERE p.slug = $1 ORDER BY sl.stem`, book)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var stem string
		if err := rows.Scan(&stem); err != nil {
			return nil, err
		}
		out = append(out, stem)
	}
	return out, rows.Err()
}

func (st *Store) LoadSetlist(book, stem string) (*setlist.Setlist, string, error) {
	var body string
	var version int
	err := st.db.QueryRow(context.Background(), `
		SELECT sl.body, sl.version FROM setlists sl JOIN projects p ON p.id = sl.project_id
		WHERE p.slug = $1 AND sl.stem = $2`, book, stem).Scan(&body, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", fmt.Errorf("no such setlist %s/%s: %w", book, stem, ErrNotFound)
	}
	if err != nil {
		return nil, "", err
	}
	return setlist.Parse(body), strconv.Itoa(version), nil
}

func (st *Store) SaveSetlist(book, stem string, l *setlist.Setlist, expectVersion string) (string, error) {
	ctx := context.Background()
	pid, err := st.projectID(ctx, book)
	if err != nil {
		return "", err
	}
	body := setlist.Write(l)
	title := l.Get("title")

	var newVersion int
	err = st.db.QueryRow(ctx, `
		INSERT INTO setlists (project_id, stem, title, body) VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, stem) DO UPDATE SET
			body = EXCLUDED.body, title = EXCLUDED.title, version = setlists.version + 1
			WHERE $5 = '' OR setlists.version::text = $5
		RETURNING version`,
		pid, stem, title, body, expectVersion).Scan(&newVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConflict
	}
	if err != nil {
		return "", err
	}
	return strconv.Itoa(newVersion), nil
}

// ImportSetlist inserts a setlist row directly (used by the import tool
// and by zip import).
func (st *Store) ImportSetlist(book, stem, body string) error {
	ctx := context.Background()
	pid, err := st.projectID(ctx, book)
	if err != nil {
		return err
	}
	title := setlist.Parse(body).Get("title")
	_, err = st.db.Exec(ctx, `
		INSERT INTO setlists (project_id, stem, title, body) VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, stem) DO NOTHING`,
		pid, stem, title, body)
	return err
}

// --- Phase 3: setlist scope (person/band-owned setlists, addressed by
// id rather than book/stem) and gigs -----------------------------------

// SetlistMeta is a setlist's scope/kind/ownership metadata — everything
// about a setlist other than its parsed entries, as the id-addressed
// routes (practice/listening, and person/band CRUD) need it. Book is ""
// for a person/band-scoped setlist (project_id is NULL for those).
type SetlistMeta struct {
	ID            int64
	Scope         string // "project", "person", "band"
	Kind          string // "performance", "themed", "practice"
	Reusable      bool
	Notes         string
	Title         string
	Book          string
	Stem          string
	OwnerPersonID *int64
	OwnerBandID   *int64
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
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	Notes         string `json:"notes"`
	Reusable      bool   `json:"reusable"`
	Body          string `json:"body"`
	OwnerPersonID *int64 `json:"person_id"`
	OwnerBandID   *int64 `json:"band_id"`
}

// GetSetlistMeta loads one setlist's scope/kind/ownership metadata by id
// — works for any scope, project-scoped setlists included.
func (st *Store) GetSetlistMeta(ctx context.Context, id int64) (*SetlistMeta, error) {
	var m SetlistMeta
	var book *string
	err := st.db.QueryRow(ctx, `
		SELECT sl.id, sl.scope::text, sl.kind::text, sl.reusable, sl.notes, sl.title,
		       p.slug, sl.stem, sl.owner_person_id, sl.owner_band_id, sl.version, sl.created_at
		FROM setlists sl LEFT JOIN projects p ON p.id = sl.project_id
		WHERE sl.id = $1`, id).
		Scan(&m.ID, &m.Scope, &m.Kind, &m.Reusable, &m.Notes, &m.Title,
			&book, &m.Stem, &m.OwnerPersonID, &m.OwnerBandID, &m.Version, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such setlist %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	if book != nil {
		m.Book = *book
	}
	return &m, nil
}

// LoadSetlistByID loads one setlist's parsed entries plus its metadata,
// by id — the resolution path practice/listening use, which works
// identically regardless of scope (project, person, or band).
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
// content — project scope: any current project member (viewer+); person
// scope: the owner only; band scope: any current band member.
func (st *Store) CanViewSetlistByID(ctx context.Context, personID, setlistID int64) (bool, error) {
	meta, err := st.GetSetlistMeta(ctx, setlistID)
	if err != nil {
		return false, err
	}
	switch meta.Scope {
	case "project":
		_, ok, err := st.GetRole(ctx, personID, meta.Book)
		return ok, err
	case "person":
		return meta.OwnerPersonID != nil && *meta.OwnerPersonID == personID, nil
	case "band":
		if meta.OwnerBandID == nil {
			return false, nil
		}
		return st.IsCurrentBandMember(ctx, *meta.OwnerBandID, personID)
	default:
		return false, nil
	}
}

// CanEditSetlistByID reports whether personID may edit setlistID —
// project scope: editor/owner project role; person scope: the owner only
// (no sharing — it's their personal setlist); band scope: any current
// band member (same open-collaboration rule as band repertoire).
func (st *Store) CanEditSetlistByID(ctx context.Context, personID, setlistID int64) (bool, error) {
	meta, err := st.GetSetlistMeta(ctx, setlistID)
	if err != nil {
		return false, err
	}
	switch meta.Scope {
	case "project":
		role, ok, err := st.GetRole(ctx, personID, meta.Book)
		if err != nil {
			return false, err
		}
		return ok && (role == "editor" || role == "owner"), nil
	case "person":
		return meta.OwnerPersonID != nil && *meta.OwnerPersonID == personID, nil
	case "band":
		if meta.OwnerBandID == nil {
			return false, nil
		}
		return st.IsCurrentBandMember(ctx, *meta.OwnerBandID, personID)
	default:
		return false, nil
	}
}

// CreateSetlist creates a new person- or band-scoped setlist (project-
// scoped setlists are still created via SaveSetlist's upsert-on-book/stem
// path — unchanged). Exactly one of ownerPersonID/ownerBandID must be
// set, matching scope. Returns the new setlist's id.
func (st *Store) CreateSetlist(ctx context.Context, scope, kind, title, notes string, reusable bool, ownerPersonID, ownerBandID *int64, actorPersonID int64) (int64, error) {
	if scope != "person" && scope != "band" {
		return 0, fmt.Errorf("invalid setlist scope %q for CreateSetlist (project-scoped setlists are created via the existing book/stem path)", scope)
	}
	if kind != "performance" && kind != "themed" && kind != "practice" {
		return 0, fmt.Errorf("invalid setlist kind %q", kind)
	}
	if scope == "person" && ownerPersonID == nil {
		return 0, fmt.Errorf("owner_person_id is required for a person-scoped setlist")
	}
	if scope == "band" && ownerBandID == nil {
		return 0, fmt.Errorf("owner_band_id is required for a band-scoped setlist")
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO setlists (project_id, stem, title, body, scope, owner_person_id, owner_band_id, kind, reusable, notes)
		VALUES (NULL, '', $1, '', $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		title, scope, ownerPersonID, ownerBandID, kind, reusable, notes).Scan(&id); err != nil {
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

// SaveSetlistByID edits a setlist's title/notes/kind/reusable fields and
// its entries, by id — the path person/band-scoped setlists are edited
// through (project-scoped setlists keep using SaveSetlist's book/stem
// path unchanged). If expectVersion is non-empty, the save is rejected
// with ErrConflict when the row's current version doesn't match.
func (st *Store) SaveSetlistByID(ctx context.Context, id int64, l *setlist.Setlist, title, notes, kind string, reusable bool, expectVersion string, actorPersonID int64) (string, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var curScope, curKind, curTitle, curNotes, curBody string
	var curReusable bool
	var curOwnerPerson, curOwnerBand *int64
	var curVersion int
	err = tx.QueryRow(ctx, `
		SELECT scope::text, kind::text, title, notes, reusable, body, owner_person_id, owner_band_id, version
		FROM setlists WHERE id = $1 FOR UPDATE`, id).
		Scan(&curScope, &curKind, &curTitle, &curNotes, &curReusable, &curBody, &curOwnerPerson, &curOwnerBand, &curVersion)
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
		Exists: true, Scope: curScope, Kind: curKind, Title: curTitle, Notes: curNotes,
		Reusable: curReusable, Body: curBody, OwnerPersonID: curOwnerPerson, OwnerBandID: curOwnerBand,
	}); err != nil {
		return "", err
	}

	body := setlist.Write(l)
	var newVersion int
	if err := tx.QueryRow(ctx, `
		UPDATE setlists SET title = $1, notes = $2, kind = $3, reusable = $4, body = $5, version = version + 1
		WHERE id = $6
		RETURNING version`,
		title, notes, kind, reusable, body, id).Scan(&newVersion); err != nil {
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

func (st *Store) listSetlistsByOwner(ctx context.Context, whereClause string, ownerID int64) ([]SetlistMeta, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, scope::text, kind::text, reusable, notes, title, stem, owner_person_id, owner_band_id, version, created_at
		FROM setlists WHERE `+whereClause+` ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SetlistMeta
	for rows.Next() {
		var m SetlistMeta
		if err := rows.Scan(&m.ID, &m.Scope, &m.Kind, &m.Reusable, &m.Notes, &m.Title, &m.Stem,
			&m.OwnerPersonID, &m.OwnerBandID, &m.Version, &m.CreatedAt); err != nil {
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
