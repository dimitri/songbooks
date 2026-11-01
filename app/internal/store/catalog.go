package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// dbtx is the subset of *pgxpool.Pool and pgx.Tx this file needs, so
// findOrCreateWork can run either against the pool directly or inside an
// already-open transaction (fork/create paths that need the work lookup
// and the variant insert to be atomic).
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Work is a lightweight title+artist identity, the catalog's backbone for
// "browse by artist/alphabetical" and "all versions of this song" views.
// Not every variant needs a hand-picked work — FindOrCreateWork
// auto-creates one from a variant's own title/artist when it's created.
type Work struct {
	ID     int64
	Title  string
	Artist string
}

// findOrCreateWork matches title/artist against works by simple
// normalization (lowercase, trim) and returns the existing work's id, or
// creates one. The ON CONFLICT ... DO UPDATE RETURNING trick makes this
// race-safe against a concurrent insert of the same normalized pair.
func (st *Store) findOrCreateWork(ctx context.Context, q dbtx, title, artist string) (int64, error) {
	var id int64
	err := q.QueryRow(ctx, `
		INSERT INTO works (title, artist) VALUES ($1, $2)
		ON CONFLICT (norm_title, norm_artist) DO UPDATE SET title = works.title
		RETURNING id`, title, artist).Scan(&id)
	return id, err
}

// FindOrCreateWork matches title/artist against the works table (simple
// lowercase+trim normalization) and returns the existing work's id, or
// creates a new one. Used whenever a variant is created without an
// explicit work already chosen for it.
func (st *Store) FindOrCreateWork(ctx context.Context, title, artist string) (int64, error) {
	return st.findOrCreateWork(ctx, st.db, title, artist)
}

// CreateWork explicitly creates (or, if one already matches by
// normalized title+artist, returns) a work. Exposed separately from
// FindOrCreateWork for callers (e.g. a future "create work" UI action)
// that want to talk about "creating a work" rather than "resolving a
// variant's work" — the underlying matching rule is identical.
func (st *Store) CreateWork(ctx context.Context, title, artist string) (int64, error) {
	return st.findOrCreateWork(ctx, st.db, title, artist)
}

// ListWorks lists every work, alphabetically by title.
func (st *Store) ListWorks(ctx context.Context) ([]Work, error) {
	rows, err := st.db.Query(ctx, `SELECT id, title, artist FROM works ORDER BY title, artist`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Work
	for rows.Next() {
		var w Work
		if err := rows.Scan(&w.ID, &w.Title, &w.Artist); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// CatalogVariant is one row of a catalog browse listing: a variant
// (songs row) plus the work/project context a browse page shows it with.
type CatalogVariant struct {
	VariantID       int64
	WorkID          int64
	Title           string
	Artist          string
	Book            string // project slug the variant row lives in
	Stem            string
	Visibility      string
	OwnerID         *int64
	OwnerBandID     *int64
	OwnerJamEventID *int64
	ForkedFromID    *int64
}

// CatalogFilter narrows ListCatalog's listing. Zero values mean "no
// filter" for that field.
type CatalogFilter struct {
	Artist  string // exact match (case-sensitive on the stored value, as typed in the filter UI's dropdown)
	Project string // project (book) slug
	Sort    string // "artist" sorts by artist then title; anything else (including "") sorts alphabetically by title
}

// ListCatalog lists every global variant plus viewerID's own
// user-visibility variants — "the whole catalog, as this person can see
// it" — filtered/sorted per filter. "By album" isn't offered: no song in
// this app's data model carries an album field (see song.MetaKeys), so
// there's nothing to group or filter by; "by project" stands in as the
// closest grouping concept that does exist.
//
// visibility='jam' rows are never included here (nor in
// ListVariantsForWork below), deliberately, mirroring how a 'band'
// variant never shows up in another band's view — a jam-scoped variant
// is only reachable through its own jam's song list (store.ListJamSongs),
// never the general catalog/work-versions browse pages.
func (st *Store) ListCatalog(ctx context.Context, viewerID int64, filter CatalogFilter) ([]CatalogVariant, error) {
	order := "s.title, s.artist"
	if filter.Sort == "artist" {
		order = "s.artist, s.title"
	}
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, p.slug, s.stem, s.visibility::text, s.owner_person_id, s.owner_band_id, s.owner_jam_event_id, s.forked_from_id
		FROM songs s JOIN projects p ON p.id = s.project_id
		WHERE (s.visibility = 'global' OR s.owner_person_id = $1)
		  AND ($2 = '' OR s.artist = $2)
		  AND ($3 = '' OR p.slug = $3)
		ORDER BY `+order, viewerID, filter.Artist, filter.Project)
	if err != nil {
		return nil, err
	}
	return scanCatalogVariants(rows)
}

// ListVariantsForWork lists every variant of one work that viewerID may
// see — every global variant, plus the viewer's own private ones — for
// the work's "all versions of this song" page. Excludes visibility='jam'
// rows — see ListCatalog's doc comment.
func (st *Store) ListVariantsForWork(ctx context.Context, workID, viewerID int64) ([]CatalogVariant, error) {
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, p.slug, s.stem, s.visibility::text, s.owner_person_id, s.owner_band_id, s.owner_jam_event_id, s.forked_from_id
		FROM songs s JOIN projects p ON p.id = s.project_id
		WHERE s.work_id = $1 AND (s.visibility = 'global' OR s.owner_person_id = $2)
		ORDER BY s.visibility, p.slug, s.stem`, workID, viewerID)
	if err != nil {
		return nil, err
	}
	return scanCatalogVariants(rows)
}

func scanCatalogVariants(rows pgx.Rows) ([]CatalogVariant, error) {
	defer rows.Close()
	var out []CatalogVariant
	for rows.Next() {
		var v CatalogVariant
		if err := rows.Scan(&v.VariantID, &v.WorkID, &v.Title, &v.Artist, &v.Book, &v.Stem, &v.Visibility, &v.OwnerID, &v.OwnerBandID, &v.OwnerJamEventID, &v.ForkedFromID); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// VariantLookup resolves a book/stem pair to its variant and work ids —
// used by pages that need to link from "this song" to its catalog
// identity (fork/history/"all versions") without loading the whole song
// body the way LoadSong does.
func (st *Store) VariantLookup(ctx context.Context, book, stem string) (variantID, workID int64, err error) {
	err = st.db.QueryRow(ctx, `
		SELECT s.id, s.work_id FROM songs s JOIN projects p ON p.id = s.project_id
		WHERE p.slug = $1 AND s.stem = $2`, book, stem).Scan(&variantID, &workID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, fmt.Errorf("no such song %s/%s: %w", book, stem, ErrNotFound)
	}
	return variantID, workID, err
}

// CanEditVariant reports whether personID (a logged-in person) may edit
// — and revert any revision of — variantID:
//   - visibility='user': only the owner (owner_person_id match).
//   - visibility='band': any CURRENT member (core or guest alike — see
//     IsCurrentBandMember) of the owning band. This is this phase's actual
//     tightening of the placeholder below: band material is no longer
//     editable by "any logged-in person," only by the band.
//   - visibility='jam': a logged-in person has no automatic edit rights
//     here (a jam participant typically ISN'T a logged-in person at all —
//     see jam_profiles) — always false. Jam-session-authenticated edit
//     access is checked separately, via CanEditVariantAsJamParticipant,
//     by the HTTP layer (see handleUpdateVariant and friends), which
//     accepts either a normal login session or a valid jam session.
//   - visibility='global': any logged-in person. Still a deliberate,
//     known-permissive placeholder, same as phase 1 — global variants
//     have no band/role concept attached to them in THIS phase either;
//     the brief explicitly defers "who can edit a global variant" to
//     later phases (plural), so this phase only had to add the band case
//     above, not solve that one too.
func (st *Store) CanEditVariant(ctx context.Context, personID, variantID int64) (bool, error) {
	var visibility string
	var ownerID, ownerBandID *int64
	err := st.db.QueryRow(ctx, `SELECT visibility::text, owner_person_id, owner_band_id FROM songs WHERE id = $1`, variantID).
		Scan(&visibility, &ownerID, &ownerBandID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	if err != nil {
		return false, err
	}
	switch visibility {
	case "global":
		return true, nil
	case "band":
		if ownerBandID == nil {
			return false, nil
		}
		return st.IsCurrentBandMember(ctx, *ownerBandID, personID)
	case "jam":
		return false, nil
	default: // "user"
		return ownerID != nil && *ownerID == personID, nil
	}
}

// CanEditVariantAsJamParticipant reports whether a valid jam session for
// jamEventID may edit variantID — the jam-session counterpart to
// CanEditVariant above. Mirrors the band branch's "any current member"
// pattern: a jam's participant roster (a live jam session for that
// event) IS its membership for this purpose, same conceptual role
// band_members plays for visibility='band'. Only true for a variant
// that's actually scoped to THIS jam event (visibility='jam' AND
// owner_jam_event_id = jamEventID) — holding a session for jam A grants
// no rights over jam B's (or any other scope's) variants.
func (st *Store) CanEditVariantAsJamParticipant(ctx context.Context, jamEventID, variantID int64) (bool, error) {
	var visibility string
	var ownerJamEventID *int64
	err := st.db.QueryRow(ctx, `SELECT visibility::text, owner_jam_event_id FROM songs WHERE id = $1`, variantID).
		Scan(&visibility, &ownerJamEventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	if err != nil {
		return false, err
	}
	return visibility == "jam" && ownerJamEventID != nil && *ownerJamEventID == jamEventID, nil
}

// GetVariant loads one variant's full catalog row by id (not scoped to a
// book/stem the way LoadSong is) — used by the fork/annotate/"all
// versions" endpoints, which address a variant by its row id.
func (st *Store) GetVariant(ctx context.Context, variantID int64) (*CatalogVariant, error) {
	var v CatalogVariant
	err := st.db.QueryRow(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, p.slug, s.stem, s.visibility::text, s.owner_person_id, s.owner_band_id, s.owner_jam_event_id, s.forked_from_id
		FROM songs s JOIN projects p ON p.id = s.project_id
		WHERE s.id = $1`, variantID).
		Scan(&v.VariantID, &v.WorkID, &v.Title, &v.Artist, &v.Book, &v.Stem, &v.Visibility, &v.OwnerID, &v.OwnerBandID, &v.OwnerJamEventID, &v.ForkedFromID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ForkVariant creates a new variant row that's a full copy of src's
// content at fork time, with forked_from_id lineage back to src, then
// points the forker's catalog at the fork, replacing their pointer at src
// if they had one (forking from your own songbook/repertoire swaps the
// pointer, it doesn't leave both). Returns the new variant's id.
//
// scope is "user" (personal fork: visibility='user', owner_person_id =
// forkerID, personal catalog_entries pointer swapped), "band" (fork into
// a band's repertoire: visibility='band', owner_band_id = bandID, that
// band's band_catalog_entries pointer swapped instead), or "jam" (fork
// into one jam event's pickable list only: visibility='jam',
// owner_jam_event_id = bandID — reusing the same int64 parameter as the
// event id, see the doc note below — then auto-added to that event's
// jam_songs via AddJamSong with the default instrument limits, since "a
// jam-scoped variant that isn't on the jam's song list doesn't make
// sense" per the brief). forkerID must be a CURRENT member of bandID for
// a "band" fork — checked inside this same transaction (returning
// ErrForbidden if not) rather than left entirely to the HTTP layer, since
// this call also writes owner_band_id. A "jam" fork has no analogous
// membership check here: holding a valid jam session for that event IS
// the membership check, and it's the HTTP layer's job (same division of
// responsibility as "band") to have already verified the caller holds
// one for THIS eventID before calling with scope="jam".
//
// bandID doubles as "the band id" (scope="band") or "the jam event id"
// (scope="jam") — not the cleanest signature, but it keeps this one
// method's shape instead of adding a fourth parameter that's only ever
// used by one of three scopes; forkerID for scope="jam" is the jam
// profile id's owning person if any (0/unknown is fine — see
// SaveVariantByID's actorPersonID convention) and is NOT written as this
// fork's owner (there is no owner_person_id for a jam-scoped variant).
//
// The fork stays attached to src's project (songs.project_id is NOT NULL
// and FK'd, and forks aren't project material in their own right yet in
// this phase) under a generated, collision-free stem; later phases that
// decouple personal/band variants from any one project can revisit this.
func (st *Store) ForkVariant(ctx context.Context, variantID, forkerID int64, scope string, bandID int64) (int64, error) {
	if scope != "user" && scope != "band" && scope != "jam" {
		return 0, fmt.Errorf("invalid fork scope %q", scope)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if scope == "band" {
		isMember, err := st.IsCurrentBandMember(ctx, bandID, forkerID)
		if err != nil {
			return 0, err
		}
		if !isMember {
			return 0, fmt.Errorf("person %d is not a current member of band %d: %w", forkerID, bandID, ErrForbidden)
		}
	}

	var projectID, workID int64
	var stem, title, artist, referenceURL, body string
	err = tx.QueryRow(ctx, `
		SELECT project_id, work_id, stem, title, artist, reference_url, body
		FROM songs WHERE id = $1 FOR UPDATE`, variantID).
		Scan(&projectID, &workID, &stem, &title, &artist, &referenceURL, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	if err != nil {
		return 0, err
	}

	forkStem := fmt.Sprintf("%s-fork-%d", stem, time.Now().UnixNano())
	var newID int64
	var creator *int64
	if forkerID != 0 {
		creator = &forkerID
	}
	switch scope {
	case "user":
		err = tx.QueryRow(ctx, `
			INSERT INTO songs (project_id, stem, title, artist, reference_url, body, work_id,
			                    visibility, owner_person_id, forked_from_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'user', $8, $9, $8)
			RETURNING id`,
			projectID, forkStem, title, artist, referenceURL, body, workID, forkerID, variantID).Scan(&newID)
	case "band":
		err = tx.QueryRow(ctx, `
			INSERT INTO songs (project_id, stem, title, artist, reference_url, body, work_id,
			                    visibility, owner_band_id, forked_from_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'band', $8, $9, $10)
			RETURNING id`,
			projectID, forkStem, title, artist, referenceURL, body, workID, bandID, variantID, forkerID).Scan(&newID)
	case "jam":
		err = tx.QueryRow(ctx, `
			INSERT INTO songs (project_id, stem, title, artist, reference_url, body, work_id,
			                    visibility, owner_jam_event_id, forked_from_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'jam', $8, $9, $10)
			RETURNING id`,
			projectID, forkStem, title, artist, referenceURL, body, workID, bandID, variantID, creator).Scan(&newID)
	}
	if err != nil {
		return 0, err
	}

	if scope == "jam" {
		if err := st.addJamSongTx(ctx, tx, bandID, newID, DefaultInstrumentLimits()); err != nil {
			return 0, err
		}
	} else if scope == "user" {
		if _, err := tx.Exec(ctx, `
			DELETE FROM catalog_entries WHERE person_id = $1 AND variant_id = $2`, forkerID, variantID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO catalog_entries (person_id, variant_id) VALUES ($1, $2)
			ON CONFLICT (person_id, variant_id) DO NOTHING`, forkerID, newID); err != nil {
			return 0, err
		}
	} else {
		var oldEntryID int64
		var oldAddedAt time.Time
		var oldAddedBy *int64
		err = tx.QueryRow(ctx, `
			SELECT id, added_at, added_by_person_id FROM band_catalog_entries
			WHERE band_id = $1 AND variant_id = $2 FOR UPDATE`, bandID, variantID).
			Scan(&oldEntryID, &oldAddedAt, &oldAddedBy)
		if err == nil {
			if err := st.RecordRevision(ctx, tx, "band_catalog_entry", fmt.Sprint(oldEntryID), &forkerID, BandCatalogEntrySnapshot{
				Exists: true, BandID: bandID, VariantID: variantID, AddedAt: oldAddedAt, AddedByPersonID: oldAddedBy,
			}); err != nil {
				return 0, err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM band_catalog_entries WHERE id = $1`, oldEntryID); err != nil {
				return 0, err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}

		var newEntryID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO band_catalog_entries (band_id, variant_id, added_by_person_id)
			VALUES ($1, $2, $3) RETURNING id`, bandID, newID, forkerID).Scan(&newEntryID); err != nil {
			return 0, err
		}
		if err := st.RecordRevision(ctx, tx, "band_catalog_entry", fmt.Sprint(newEntryID), &forkerID, BandCatalogEntrySnapshot{Exists: false}); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return newID, nil
}

// CreateJamVariant creates a brand-new, from-scratch variant scoped to
// jamEventID only (visibility='jam', owner_jam_event_id = jamEventID) —
// the jam-scoped sibling of store.CreateSong: same creation shape
// (title/artist header plus one empty [Verse] section to start from),
// just visibility='jam' instead of 'global'/owner_jam_event_id instead of
// no owner, and auto-added to the event's own jam_songs pickable list in
// the SAME transaction via addJamSongTx — "a jam-scoped variant that
// isn't on the jam's song list doesn't make sense" per the brief, so
// creation and list-membership happen atomically, never one without the
// other. The variant still lives under projectID (the jam's own parent
// project), consistent with ForkVariant's "forks/jam variants aren't
// project material in their own right yet" rule. creatorPersonID is the
// acting participant's underlying person id if they also happen to be
// logged in; 0 (the common case — an unlogged jam_profiles participant)
// is recorded as NULL, same "unknown actor" convention as
// SaveVariantByID.
func (st *Store) CreateJamVariant(ctx context.Context, jamEventID, projectID int64, title, artist string, creatorPersonID int64) (int64, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	workID, err := st.findOrCreateWork(ctx, tx, title, artist)
	if err != nil {
		return 0, fmt.Errorf("resolving work for %q/%q: %w", title, artist, err)
	}

	stem := fmt.Sprintf("jam-%d-%d", jamEventID, time.Now().UnixNano())
	var b strings.Builder
	b.WriteString("title: " + title + "\n")
	if artist != "" {
		b.WriteString("artist: " + artist + "\n")
	}
	b.WriteString("\n[Verse]\n")

	var creator *int64
	if creatorPersonID != 0 {
		creator = &creatorPersonID
	}
	var newID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO songs (project_id, stem, title, artist, body, work_id,
		                    visibility, owner_jam_event_id, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, 'jam', $7, $8)
		RETURNING id`,
		projectID, stem, title, artist, b.String(), workID, jamEventID, creator).Scan(&newID); err != nil {
		return 0, err
	}

	if err := st.addJamSongTx(ctx, tx, jamEventID, newID, DefaultInstrumentLimits()); err != nil {
		return 0, err
	}

	return newID, tx.Commit(ctx)
}

// SaveVariantByID edits a variant's content directly by its row id,
// rather than by book/stem the way SaveSong/SaveRaw do — this is the path
// personal- and band-owned variants are edited through, since their
// editor (a fork's owner, or a band member) need not hold any
// project-level editor role on the project the variant happens to live
// under (see ForkVariant's doc comment on why forks stay attached to
// src's project regardless). Callers must check CanEditVariant
// themselves first (see handleUpdateVariant) — this method only does the
// write + revision bookkeeping, same division of responsibility as
// RevertRevision/handleRevertRevision.
//
// If expectVersion is non-empty, the save is rejected with ErrConflict
// when the row's current version doesn't match. actorPersonID (0 =
// unknown) is recorded as the revision's actor.
func (st *Store) SaveVariantByID(ctx context.Context, variantID int64, title, artist, referenceURL, body, expectVersion string, actorPersonID int64) (string, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var curTitle, curArtist, curRef, curBody string
	var curVersion int
	err = tx.QueryRow(ctx, `
		SELECT title, artist, reference_url, body, version FROM songs
		WHERE id = $1 FOR UPDATE`, variantID).
		Scan(&curTitle, &curArtist, &curRef, &curBody, &curVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
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
	if err := st.RecordRevision(ctx, tx, "variant", strconv.FormatInt(variantID, 10), actor, VariantSnapshot{
		Title: curTitle, Artist: curArtist, ReferenceURL: curRef, Body: curBody,
	}); err != nil {
		return "", err
	}

	var newVersion int
	if err := tx.QueryRow(ctx, `
		UPDATE songs SET title = $1, artist = $2, reference_url = $3, body = $4, version = version + 1
		WHERE id = $5
		RETURNING version`,
		title, artist, referenceURL, body, variantID).Scan(&newVersion); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return strconv.Itoa(newVersion), nil
}

// AddToCatalog points personID's personal catalog at variantID — "add
// this (global, or anyone's) variant to my songbook," by reference, not
// by copy. A no-op if it's already there.
func (st *Store) AddToCatalog(ctx context.Context, personID, variantID int64) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO catalog_entries (person_id, variant_id) VALUES ($1, $2)
		ON CONFLICT (person_id, variant_id) DO NOTHING`, personID, variantID)
	return err
}

// RemoveFromCatalog removes personID's pointer to variantID, if any.
func (st *Store) RemoveFromCatalog(ctx context.Context, personID, variantID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM catalog_entries WHERE person_id = $1 AND variant_id = $2`, personID, variantID)
	return err
}

// CatalogEntry is one row of a person's personal catalog ("my songbook").
type CatalogEntry struct {
	CatalogVariant
	AddedAt time.Time
}

// ListMyCatalog lists personID's personal catalog, most recently added
// first.
func (st *Store) ListMyCatalog(ctx context.Context, personID int64) ([]CatalogEntry, error) {
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, p.slug, s.stem, s.visibility::text, s.owner_person_id, s.owner_band_id, s.forked_from_id, c.added_at
		FROM catalog_entries c
		JOIN songs s ON s.id = c.variant_id
		JOIN projects p ON p.id = s.project_id
		WHERE c.person_id = $1
		ORDER BY c.added_at DESC`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CatalogEntry
	for rows.Next() {
		var e CatalogEntry
		if err := rows.Scan(&e.VariantID, &e.WorkID, &e.Title, &e.Artist, &e.Book, &e.Stem, &e.Visibility, &e.OwnerID, &e.OwnerBandID, &e.ForkedFromID, &e.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Annotation is one free-text note left on a variant.
type Annotation struct {
	ID         int64
	VariantID  int64
	PersonID   int64
	PersonName string
	Body       string
	CreatedAt  time.Time
}

// AnnotateVariant leaves a free-text note on variantID — not an edit to
// the variant's own body/grid/lyrics, just a comment attached alongside
// it. Returns the new annotation's id.
func (st *Store) AnnotateVariant(ctx context.Context, variantID, personID int64, body string) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO variant_annotations (variant_id, person_id, body) VALUES ($1, $2, $3)
		RETURNING id`, variantID, personID, body).Scan(&id)
	return id, err
}

// ListAnnotations lists every note left on variantID, oldest first (a
// simple chronological thread).
func (st *Store) ListAnnotations(ctx context.Context, variantID int64) ([]Annotation, error) {
	rows, err := st.db.Query(ctx, `
		SELECT a.id, a.variant_id, a.person_id, p.display_name, a.body, a.created_at
		FROM variant_annotations a JOIN people p ON p.id = a.person_id
		WHERE a.variant_id = $1
		ORDER BY a.created_at`, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Annotation
	for rows.Next() {
		var a Annotation
		if err := rows.Scan(&a.ID, &a.VariantID, &a.PersonID, &a.PersonName, &a.Body, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
