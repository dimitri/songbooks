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
	Genre  string
	Album  string
}

// GetWork fetches one work by id, genre and album included.
func (st *Store) GetWork(ctx context.Context, workID int64) (*Work, error) {
	var w Work
	err := st.db.QueryRow(ctx, `SELECT id, title, artist, coalesce(genre, ''), coalesce(album, '') FROM works WHERE id = $1`, workID).
		Scan(&w.ID, &w.Title, &w.Artist, &w.Genre, &w.Album)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such work %d: %w", workID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// SetWorkAlbum sets (or clears, with "") workID's free-text album — see
// migration 0033's comment for why this lives on works rather than
// per-variant.
func (st *Store) SetWorkAlbum(ctx context.Context, workID int64, album string) error {
	var arg any
	if album != "" {
		arg = album
	}
	tag, err := st.db.Exec(ctx, `UPDATE works SET album = $1 WHERE id = $2`, arg, workID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such work %d: %w", workID, ErrNotFound)
	}
	return nil
}

// SetWorkGenre sets (or clears, with "") workID's free-text genre —
// see migration 0030's comment for why this lives on works rather than
// per-variant.
func (st *Store) SetWorkGenre(ctx context.Context, workID int64, genre string) error {
	var arg any
	if genre != "" {
		arg = genre
	}
	tag, err := st.db.Exec(ctx, `UPDATE works SET genre = $1 WHERE id = $2`, arg, workID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such work %d: %w", workID, ErrNotFound)
	}
	return nil
}

// AlbumCover is one album's cover art for a band, keyed by (band_id,
// album) per migration 0041 (band_albums, renamed from project_albums).
type AlbumCover struct {
	Album       string
	CoverURL    string
	ReleaseYear int
	Credit      string
}

// GetAlbumCovers returns every album cover known for bandID, keyed by
// album name, for pages that group a repertoire by album (an
// "original"-type band's Repertoire tab and Overview preview).
func (st *Store) GetAlbumCovers(ctx context.Context, bandID int64) (map[string]AlbumCover, error) {
	rows, err := st.db.Query(ctx, `SELECT album, cover_url, coalesce(release_year, 0), credit FROM band_albums WHERE band_id = $1`, bandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]AlbumCover{}
	for rows.Next() {
		var c AlbumCover
		if err := rows.Scan(&c.Album, &c.CoverURL, &c.ReleaseYear, &c.Credit); err != nil {
			return nil, err
		}
		out[c.Album] = c
	}
	return out, rows.Err()
}

// SetAlbumCover upserts one album's cover art for a band — idempotent,
// safe to re-run (e.g. a backfill script run more than once).
func (st *Store) SetAlbumCover(ctx context.Context, bandID int64, album, coverURL string, releaseYear int, credit string) error {
	var yr any
	if releaseYear > 0 {
		yr = releaseYear
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO band_albums (band_id, album, cover_url, release_year, credit)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (band_id, album) DO UPDATE
		SET cover_url = EXCLUDED.cover_url, release_year = EXCLUDED.release_year, credit = EXCLUDED.credit
	`, bandID, album, coverURL, yr, credit)
	return err
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

// ListGenres returns every distinct non-empty genre value currently set
// on a work, alphabetically — populates the catalog page's genre filter
// dropdown. Free text (see migration 0030), so this is literally
// "whatever strings people have typed," not a fixed vocabulary.
func (st *Store) ListGenres(ctx context.Context) ([]string, error) {
	rows, err := st.db.Query(ctx, `SELECT DISTINCT genre FROM works WHERE genre IS NOT NULL AND genre <> '' ORDER BY genre`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
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
	Visibility      string
	OwnerID         *int64
	OwnerBandID     *int64
	OwnerCommunityID   *int64
	ForkedFromID    *int64
	Genre           string // from the parent work; "" means unset, not filtered out of an unfiltered listing
	Album           string // from the parent work; "" means unset (migration 0033)
}

// CatalogFilter narrows ListCatalog's listing. Zero values mean "no
// filter" for that field.
type CatalogFilter struct {
	Query  string // full-text search over title/artist/lyrics (songs.search), via plainto_tsquery
	Artist string // exact match (case-sensitive on the stored value, as typed in the filter UI's dropdown)
	Genre  string // exact match against works.genre
	Sort   string // "artist" sorts by artist then title; anything else (including "") sorts alphabetically by title
}

// ListCatalog lists every global variant plus viewerID's own
// user-visibility variants — "the whole catalog, as this person can see
// it" — filtered/sorted per filter. Each variant carries its parent
// work's album (migration 0033) for pages that group by it (an
// "original"-type band's repertoire); "by album" isn't offered as a
// CatalogFilter itself, since most variants have no album set.
//
func (st *Store) ListCatalog(ctx context.Context, viewerID int64, filter CatalogFilter) ([]CatalogVariant, error) {
	order := "s.title, s.artist"
	if filter.Sort == "artist" {
		order = "s.artist, s.title"
	}
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, s.visibility::text, s.owner_person_id, s.owner_band_id, s.owner_community_id, s.forked_from_id, coalesce(w.genre, ''), coalesce(w.album, '')
		FROM songs s JOIN works w ON w.id = s.work_id
		WHERE (s.visibility = 'global' OR s.owner_person_id = $1)
		  AND ($2 = '' OR s.artist = $2)
		  AND ($3 = '' OR w.genre = $3)
		  AND ($4 = '' OR s.search @@ plainto_tsquery('english', $4))
		ORDER BY `+order, viewerID, filter.Artist, filter.Genre, filter.Query)
	if err != nil {
		return nil, err
	}
	return scanCatalogVariants(rows)
}

// ListVariantsForWork lists every variant of one work that viewerID may
// see — every global variant, plus the viewer's own private ('user')
// ones, plus any 'band'-scoped one owned by a band viewerID is CURRENTLY
// a member of — for the work's "all versions of this song" page.
//
// Unlike ListCatalog (which deliberately leaves 'band' rows out entirely
// — a band's repertoire has its own page, ListBandCatalog, and mixing it
// into the general browse page isn't what that page is for), THIS page
// is "every version of this one song, as far as I can see it," so a
// band's own private arrangement of a song needs to actually show up
// here for that band's own members — otherwise a band that privately
// forked (or wrote from scratch) its own arrangement of a work that also
// has a global variant would never be able to find/open its own version
// from the work's "all versions" page at all. Before this fix, 'band'
// rows were excluded unconditionally here too (same filter as
// ListCatalog), which was never a leak to outsiders (nobody saw them),
// just an under-inclusion bug for the band's own members.
func (st *Store) ListVariantsForWork(ctx context.Context, workID, viewerID int64) ([]CatalogVariant, error) {
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, s.visibility::text, s.owner_person_id, s.owner_band_id, s.owner_community_id, s.forked_from_id, coalesce(w.genre, ''), coalesce(w.album, '')
		FROM songs s JOIN works w ON w.id = s.work_id
		WHERE s.work_id = $1 AND (
		  s.visibility = 'global'
		  OR (s.visibility = 'user' AND s.owner_person_id = $2)
		  OR (s.visibility = 'band' AND EXISTS (
		        SELECT 1 FROM band_members bm
		        WHERE bm.band_id = s.owner_band_id AND bm.person_id = $2 AND upper_inf(bm.validity)))
		  OR (s.visibility = 'community' AND EXISTS (
		        SELECT 1 FROM community_members sm
		        WHERE sm.community_id = s.owner_community_id AND sm.person_id = $2 AND upper_inf(sm.validity)))
		)
		ORDER BY s.visibility, s.id`, workID, viewerID)
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
		if err := rows.Scan(&v.VariantID, &v.WorkID, &v.Title, &v.Artist, &v.Visibility, &v.OwnerID, &v.OwnerBandID, &v.OwnerCommunityID, &v.ForkedFromID, &v.Genre, &v.Album); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CanEditVariant reports whether personID (a logged-in person) may edit
// — and revert any revision of — variantID:
//   - visibility='user': only the owner (owner_person_id match).
//   - visibility='band': any CURRENT member (core or guest alike — see
//     IsCurrentBandMember) of the owning band. This is this phase's actual
//     tightening of the placeholder below: band material is no longer
//     editable by "any logged-in person," only by the band.
//   - visibility='community': any CURRENT member — matches band's own
//     model (see the community-page member-level catalog change).
//   - visibility='global': any logged-in person. Still a deliberate,
//     known-permissive placeholder, same as phase 1 — global variants
//     have no band/role concept attached to them in THIS phase either;
//     the brief explicitly defers "who can edit a global variant" to
//     later phases (plural), so this phase only had to add the band case
//     above, not solve that one too.
func (st *Store) CanEditVariant(ctx context.Context, personID, variantID int64) (bool, error) {
	var visibility string
	var ownerID, ownerBandID, ownerCommunityID, ownerSetlistID *int64
	err := st.db.QueryRow(ctx, `SELECT visibility::text, owner_person_id, owner_band_id, owner_community_id, owner_setlist_id FROM songs WHERE id = $1`, variantID).
		Scan(&visibility, &ownerID, &ownerBandID, &ownerCommunityID, &ownerSetlistID)
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
	case "community":
		if ownerCommunityID == nil {
			return false, nil
		}
		return st.canViewCommunityVariant(ctx, *ownerCommunityID, variantID, personID)
	case "setlist":
		if ownerSetlistID == nil {
			return false, nil
		}
		return st.CanEditSetlistByID(ctx, personID, *ownerSetlistID)
	default: // "user"
		return ownerID != nil && *ownerID == personID, nil
	}
}

// CanViewVariant is CanEditVariant's read-only counterpart — the same
// visibility/owner switch, but at the lower "may look at it" bar rather
// than "may change it": every visibility that CanEditVariant already
// grants edit access to is trivially viewable too, plus two cases
// CanEditVariant deliberately keeps narrower for editing:
//   - visibility='community': any CURRENT member (student or instructor),
//     not instructor-only — CanEditVariant's own doc comment already
//     states this as the intended asymmetry ("a current STUDENT may view
//     the community's material... but never edit it directly"); this is
//     that view path.
//   - visibility='user': unchanged from CanEditVariant (owner-only to
//     view, same as to edit).
//
// Added alongside requireSongRole (auth.go) — before this, EVERY song
// page's HTTP-layer gate (GET /b/{book}/{stem} and its API/export
// siblings) checked ONLY project-role membership via requireRole, with
// no awareness of band/community ownership at all. A band- or community-owned
// song's underlying project (songs.project_id — see ForkVariant's and
// CreateBandVariant's own doc comments on why a fork/band-variant stays
// attached to SOME project rather than having one of its own) routinely
// has zero project_members rows for the very people who should be able
// to see it: its own band's members. The project-role gate alone made
// such a song unreachable by anyone — including, in the reported case,
// the band member who had just created it — a 403 on every click,
// regardless of real (band-membership-based) access rights that
// CanEditVariant/CanViewVariant already correctly compute. requireAuth
// still gates "is anyone logged in" first, as before.
func (st *Store) CanViewVariant(ctx context.Context, personID, variantID int64) (bool, error) {
	var visibility string
	var ownerID, ownerBandID, ownerCommunityID, ownerSetlistID *int64
	err := st.db.QueryRow(ctx, `SELECT visibility::text, owner_person_id, owner_band_id, owner_community_id, owner_setlist_id FROM songs WHERE id = $1`, variantID).
		Scan(&visibility, &ownerID, &ownerBandID, &ownerCommunityID, &ownerSetlistID)
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
	case "community":
		if ownerCommunityID == nil {
			return false, nil
		}
		return st.canViewCommunityVariant(ctx, *ownerCommunityID, variantID, personID)
	case "setlist":
		if ownerSetlistID == nil {
			return false, nil
		}
		return st.CanViewSetlistByID(ctx, personID, *ownerSetlistID)
	default: // "user"
		return ownerID != nil && *ownerID == personID, nil
	}
}

// canViewCommunityVariant is CanViewVariant/CanEditVariant's shared
// "community"-visibility case: plain community membership, UNLESS every
// songbook that actually contains this variant is gated (RequiresGrant —
// the paywall case), in which case personID also needs a grant on at
// least one of them (or a manage-type privilege — staff can always see
// what they administer). A variant with no songbook_entries row at all
// (shouldn't normally happen, but CreateVariant/ImportVariant don't
// strictly guarantee one) falls back to plain membership — there's no
// containing songbook to gate on.
func (st *Store) canViewCommunityVariant(ctx context.Context, communityID, variantID, personID int64) (bool, error) {
	isMember, err := st.IsCurrentCommunityMember(ctx, communityID, personID)
	if err != nil || !isMember {
		return false, err
	}
	rows, err := st.db.Query(ctx, `
		SELECT sb.id, sb.requires_grant
		FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		WHERE se.variant_id = $1 AND sb.owner_kind = 'community' AND sb.owner_community_id = $2`,
		variantID, communityID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var anyGated bool
	var gatedIDs []int64
	for rows.Next() {
		var sbID int64
		var requiresGrant bool
		if err := rows.Scan(&sbID, &requiresGrant); err != nil {
			return false, err
		}
		if !requiresGrant {
			return true, nil // open songbook contains it — access via that one
		}
		anyGated = true
		gatedIDs = append(gatedIDs, sbID)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if !anyGated {
		return true, nil // no containing songbook at all, or none gated
	}
	if isStaff, err := st.HasAnyCommunityManagePrivilege(ctx, communityID, personID); err != nil {
		return false, err
	} else if isStaff {
		return true, nil
	}
	for _, sbID := range gatedIDs {
		if granted, err := st.HasSongbookAccessGrant(ctx, sbID, personID); err != nil {
			return false, err
		} else if granted {
			return true, nil
		}
	}
	return false, nil
}

// GetVariant loads one variant's full catalog row by id (not scoped to a
// book/stem the way LoadSong is) — used by the fork/annotate/"all
// versions" endpoints, which address a variant by its row id.
func (st *Store) GetVariant(ctx context.Context, variantID int64) (*CatalogVariant, error) {
	var v CatalogVariant
	err := st.db.QueryRow(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, s.visibility::text, s.owner_person_id, s.owner_band_id, s.owner_community_id, s.forked_from_id
		FROM songs s WHERE s.id = $1`, variantID).
		Scan(&v.VariantID, &v.WorkID, &v.Title, &v.Artist, &v.Visibility, &v.OwnerID, &v.OwnerBandID, &v.OwnerCommunityID, &v.ForkedFromID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// VariantContent loads one variant's full editable-fields content
// (title/artist/reference_url/body) by id — GetVariant's catalog-browse
// shape doesn't carry body, and this is the one other place that needs
// it: applying an accepted community_submissions 'edit' onto its source
// variant (see AcceptCommunitySubmission).
func (st *Store) VariantContent(ctx context.Context, variantID int64) (title, artist, referenceURL, body string, err error) {
	err = st.db.QueryRow(ctx, `SELECT title, artist, reference_url, body FROM songs WHERE id = $1`, variantID).
		Scan(&title, &artist, &referenceURL, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", "", fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	return title, artist, referenceURL, body, err
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
// band's band_catalog_entries pointer swapped instead), or "community"
// (fork into a community's repertoire: visibility='community',
// owner_community_id = bandID, that community's community_catalog_entries
// pointer swapped instead — any current member, same shape/gating as
// "band"). forkerID must be a CURRENT member of bandID for a "band" or
// "community" fork — checked inside this same transaction (returning
// ErrForbidden if not) rather than left entirely to the HTTP layer, since
// this call also writes owner_band_id/owner_community_id.
//
// bandID doubles as "the band id" (scope="band") or "the community id"
// (scope="community") — not the cleanest signature, but it keeps this
// one method's shape instead of adding a parameter only one scope uses.
//
// The fork stays attached to src's project (songs.project_id is NOT NULL
// and FK'd, and forks aren't project material in their own right yet in
// this phase) under a generated, collision-free stem; later phases that
// decouple personal/band variants from any one project can revisit this.
func (st *Store) ForkVariant(ctx context.Context, variantID, forkerID int64, scope string, bandID int64) (int64, error) {
	if scope != "user" && scope != "band" && scope != "community" {
		return 0, fmt.Errorf("invalid fork scope %q", scope)
	}

	// Resolved before the transaction (GetDefaultSongbook isn't tx-scoped,
	// and may itself need to INSERT a songbook — "every person/band gets
	// a default songbook automatically" doesn't hold for an account that
	// forks something before ever visiting its own songbook page, e.g.
	// straight from the catalog browse page or the new-song wizard's
	// catalog-match step; the raw `... AND is_default` SELECT this used
	// to run inline hit exactly that gap and 500'd with a bare "no rows
	// in result set").
	var defaultSongbookID int64
	switch scope {
	case "user":
		var err error
		defaultSongbookID, err = st.GetDefaultSongbook(ctx, "person", forkerID)
		if err != nil {
			return 0, err
		}
	case "band":
		var err error
		defaultSongbookID, err = st.GetDefaultSongbook(ctx, "band", bandID)
		if err != nil {
			return 0, err
		}
	case "community":
		var err error
		defaultSongbookID, err = st.GetDefaultSongbook(ctx, "community", bandID)
		if err != nil {
			return 0, err
		}
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if scope == "band" {
		// isCurrentBandMember(ctx, tx, ...), not st.IsCurrentBandMember —
		// tx is already open, so this check must run on that same
		// connection rather than reach back out to the pool (see
		// isCurrentBandMember's doc comment in bands.go).
		isMember, err := isCurrentBandMember(ctx, tx, bandID, forkerID)
		if err != nil {
			return 0, err
		}
		if !isMember {
			return 0, fmt.Errorf("person %d is not a current member of band %d: %w", forkerID, bandID, ErrForbidden)
		}
	} else if scope == "community" {
		// Any current member, same as "band" above — not staff-only:
		// a community's repertoire is member-editable the same way a
		// band's is, no instructor/student review gate. bandID doubles
		// as "the community id" for this scope.
		isMember, err := isCurrentCommunityMember(ctx, tx, bandID, forkerID, "")
		if err != nil {
			return 0, err
		}
		if !isMember {
			return 0, fmt.Errorf("person %d is not a current member of community %d: %w", forkerID, bandID, ErrForbidden)
		}
	}

	var workID int64
	var title, artist, referenceURL, body string
	err = tx.QueryRow(ctx, `
		SELECT work_id, title, artist, reference_url, body
		FROM songs WHERE id = $1 FOR UPDATE`, variantID).
		Scan(&workID, &title, &artist, &referenceURL, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	if err != nil {
		return 0, err
	}

	var newID int64
	switch scope {
	case "user":
		err = tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, reference_url, body, work_id,
			                    visibility, owner_person_id, forked_from_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, $5, 'user', $6, $7, $6)
			RETURNING id`,
			title, artist, referenceURL, body, workID, forkerID, variantID).Scan(&newID)
	case "band":
		err = tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, reference_url, body, work_id,
			                    visibility, owner_band_id, forked_from_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, $5, 'band', $6, $7, $8)
			RETURNING id`,
			title, artist, referenceURL, body, workID, bandID, variantID, forkerID).Scan(&newID)
	case "community":
		err = tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, reference_url, body, work_id,
			                    visibility, owner_community_id, forked_from_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, $5, 'community', $6, $7, $8)
			RETURNING id`,
			title, artist, referenceURL, body, workID, bandID, variantID, forkerID).Scan(&newID)
	}
	if err != nil {
		return 0, err
	}

	switch scope {
	case "user":
		if _, err := tx.Exec(ctx, `DELETE FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`, defaultSongbookID, variantID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)
			ON CONFLICT (songbook_id, variant_id) DO NOTHING`, defaultSongbookID, newID, forkerID); err != nil {
			return 0, err
		}
	case "band":
		if _, err := tx.Exec(ctx, `DELETE FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`, defaultSongbookID, variantID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)`,
			defaultSongbookID, newID, forkerID); err != nil {
			return 0, err
		}
	case "community": // same songbook_entries pointer-swap shape as "band" — see migration 0042, which dropped community_catalog_entries in favor of this.
		if _, err := tx.Exec(ctx, `DELETE FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`, defaultSongbookID, variantID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)`,
			defaultSongbookID, newID, forkerID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return newID, nil
}

// CreateBandVariant creates a brand-new, from-scratch variant owned by
// bandID (visibility='band', owner_band_id = bandID) — the band-scoped
// sibling of store.CreateSong: same creation shape (title/artist header
// plus one empty [Verse] section to start from), just visibility='band'/
// owner_band_id instead of 'global'/no owner. Before this method
// existed, the only way a band acquired band-owned material was
// ForkVariant with scope="band" — which requires an existing source
// variant to fork from.
func (st *Store) CreateBandVariant(ctx context.Context, bandID int64, title, artist string, creatorPersonID int64) (int64, error) {
	// Resolved before the transaction — same reasoning as ForkVariant's
	// own fix: GetDefaultSongbook isn't tx-scoped and may itself need to
	// INSERT a songbook row (a band's default songbook is created
	// lazily, same as a person's). The raw `... AND is_default` SELECT
	// this used to run inline assumed one always already existed and
	// 500'd with a bare "no rows in result set" for a band whose
	// songbook page had never been visited — exactly the gap flagged:
	// every "create a song" path needs a real target songbook to add
	// into, not an assumption that one exists.
	sbID, err := st.GetDefaultSongbook(ctx, "band", bandID)
	if err != nil {
		return 0, err
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// isCurrentBandMember(ctx, tx, ...), not st.IsCurrentBandMember — tx is
	// already open (see isCurrentBandMember's doc comment in bands.go).
	isMember, err := isCurrentBandMember(ctx, tx, bandID, creatorPersonID)
	if err != nil {
		return 0, err
	}
	if !isMember {
		return 0, fmt.Errorf("person %d is not a current member of band %d: %w", creatorPersonID, bandID, ErrForbidden)
	}

	workID, err := st.findOrCreateWork(ctx, tx, title, artist)
	if err != nil {
		return 0, fmt.Errorf("resolving work for %q/%q: %w", title, artist, err)
	}

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
		INSERT INTO songs (title, artist, body, work_id, visibility, owner_band_id, created_by_person_id)
		VALUES ($1, $2, $3, $4, 'band', $5, $6)
		RETURNING id`,
		title, artist, b.String(), workID, bandID, creator).Scan(&newID); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)`,
		sbID, newID, creator); err != nil {
		return 0, err
	}

	return newID, tx.Commit(ctx)
}

// CreateCommunityVariant creates a brand-new, from-scratch variant owned by
// communityID (visibility='community', owner_community_id = communityID) — the
// community-scoped sibling of CreateBandVariant, for a member who wants
// to write a brand-new original straight into the community's repertoire
// without first finding something to fork. Any current member
// (creatorPersonID must be a CURRENT MEMBER of communityID — checked
// inside this same tx, same isCurrentCommunityMember(ctx, tx, ...) pattern
// CreateBandVariant/ForkVariant use).
func (st *Store) CreateCommunityVariant(ctx context.Context, communityID int64, title, artist string, creatorPersonID int64) (int64, error) {
	// Resolved before the transaction — same ForkVariant/CreateBandVariant
	// fix: GetDefaultSongbook isn't tx-scoped and may itself need to
	// INSERT a songbook row. This also fixes a real, separate bug this
	// function had: it used to write straight to community_catalog_entries,
	// a table migration 0042 DROPPED in favor of songbook_entries —
	// every call to this function has been failing outright in
	// production since that migration shipped.
	sbID, err := st.GetDefaultSongbook(ctx, "community", communityID)
	if err != nil {
		return 0, err
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// Any current member, same as CreateBandVariant — not staff-only:
	// a community's repertoire is member-editable the same way a
	// band's is, no instructor/student review gate.
	isMember, err := isCurrentCommunityMember(ctx, tx, communityID, creatorPersonID, "")
	if err != nil {
		return 0, err
	}
	if !isMember {
		return 0, fmt.Errorf("person %d is not a current member of community %d: %w", creatorPersonID, communityID, ErrForbidden)
	}

	workID, err := st.findOrCreateWork(ctx, tx, title, artist)
	if err != nil {
		return 0, fmt.Errorf("resolving work for %q/%q: %w", title, artist, err)
	}

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
		INSERT INTO songs (title, artist, body, work_id, visibility, owner_community_id, created_by_person_id)
		VALUES ($1, $2, $3, $4, 'community', $5, $6)
		RETURNING id`,
		title, artist, b.String(), workID, communityID, creator).Scan(&newID); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)`,
		sbID, newID, creator); err != nil {
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

// AddToCatalog/RemoveFromCatalog/ListMyCatalog are superseded by
// songbooks.go's AddToSongbook/RemoveFromSongbook/ListSongbookEntries,
// called against GetDefaultSongbook(ctx, "person", personID) for "my
// songbook" — a person can now have more than one songbook, so a
// person-id-keyed shortcut doesn't fit any more.

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
// GlobalVariantStub is just enough of a global-visibility variant to
// drive a batch re-fetch/redo tool (cmd/bandscampredo) — id/title/
// artist, nothing else.
type GlobalVariantStub struct {
	ID            int64
	Title, Artist string
}

// FindGlobalVariantByWorkID looks for an existing global-visibility
// song for workID — used by a redo/import tool to update an existing
// catalog entry in place (preserving its id, so anything already
// pointing at it stays valid) instead of creating a duplicate.
func (st *Store) FindGlobalVariantByWorkID(ctx context.Context, workID int64) (variantID int64, found bool, err error) {
	// ORDER BY id — not just LIMIT 1 — so this is deterministic: a
	// work with more than one global variant (a real, pre-existing
	// state: the original bulk import that seeded the catalog created
	// at least two such duplicate pairs) always resolves to the SAME
	// one across repeated runs of a redo/import tool. Without this, two
	// runs of the same tool could each pick a different row (Postgres
	// gives no ordering guarantee to a bare LIMIT 1) and end up linking
	// BOTH variants into a songbook instead of reusing one — exactly
	// what happened once before this was added.
	err = st.db.QueryRow(ctx, `
		SELECT id FROM songs WHERE work_id = $1 AND visibility = 'global' ORDER BY id LIMIT 1`, workID).Scan(&variantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return variantID, true, nil
}

// ListGlobalVariantsByBatchTimestamp lists every global-visibility
// song whose created_at (truncated to the second) matches batchSecond
// — a bulk import lands every row in one INSERT within the same
// second, so this isolates one specific batch from the rest of the
// global catalog without needing a separate "which batch" column.
func (st *Store) ListGlobalVariantsByBatchTimestamp(ctx context.Context, batchSecond time.Time) ([]GlobalVariantStub, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, title, artist FROM songs
		WHERE visibility = 'global' AND date_trunc('second', created_at) = date_trunc('second', $1::timestamptz)
		ORDER BY title`, batchSecond)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GlobalVariantStub
	for rows.Next() {
		var v GlobalVariantStub
		if err := rows.Scan(&v.ID, &v.Title, &v.Artist); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

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
