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
// "browse by artist/alphabetical" and "this song’s recordings and arrangements" views.
// Not every variant needs a hand-picked work — FindOrCreateWork
// auto-creates one from a variant's own title/artist when it's created.
type Work struct {
	ID     int64
	Title  string
	Artist string
	Genre  string
}

// GetWork fetches one work by id, genre and album included.
func (st *Store) GetWork(ctx context.Context, workID int64) (*Work, error) {
	var w Work
	err := st.db.QueryRow(ctx, `SELECT id, title, artist, coalesce(genre, '') FROM works WHERE id = $1`, workID).
		Scan(&w.ID, &w.Title, &w.Artist, &w.Genre)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such work %d: %w", workID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
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
	VariantID int64
	WorkID    int64
	Title     string
	Artist    string
	// Visibility is the song's kind, from its owner (songKindSQL):
	// "global" (The Big Songbook's, the catalogue), "user", "band",
	// "community" — a label; SongRightsFor says who may do what.
	Visibility       string
	Public           bool
	VersionName      string // "" for a song's one version (migration 0113)
	OwnerID          *int64
	OwnerBandID      *int64
	OwnerCommunityID *int64
	ForkedFromID     *int64
	Genre            string // from the parent work; "" means unset, not filtered out of an unfiltered listing
	Album            string // the first album it is on (albums, migration 0146); "" when none
	// BandName is the band's, for a band's version shown in place of the
	// catalogue's song (PreferBandVersions); "" otherwise.
	BandName string
}

// CatalogFilter narrows ListCatalog's listing. Zero values mean "no
// filter" for that field.
type CatalogFilter struct {
	Query  string // full-text search over title/artist/lyrics (songs.search), via plainto_tsquery
	Artist string // exact match (case-sensitive on the stored value, as typed in the filter UI's dropdown)
	Genre  string // exact match against works.genre
	Sort   string // "artist" sorts by artist then title; anything else (including "") sorts alphabetically by title
}

// ListCatalog lists every public song plus viewerID's own — "the whole
// catalog, as this person can see it" — filtered/sorted per filter. Each variant carries its parent
// work's album (migration 0033) for pages that group by it (an
// "original"-type band's repertoire); "by album" isn't offered as a
// CatalogFilter itself, since most variants have no album set.
func (st *Store) ListCatalog(ctx context.Context, viewerID int64, filter CatalogFilter) ([]CatalogVariant, error) {
	order := "s.title, s.artist"
	if filter.Sort == "artist" {
		order = "s.artist, s.title"
	}
	rows, err := st.db.Query(ctx, `
		SELECT `+catalogVariantCols+`, coalesce(w.genre, ''), `+songAlbumSQL("s")+`
		FROM songs s JOIN works w ON w.id = s.work_id
		WHERE (s.public OR s.owner_person_id = $1)
		  AND ($2 = '' OR s.artist = $2)
		  AND ($3 = '' OR w.genre = $3)
		  AND ($4 = '' OR s.search @@ plainto_tsquery('english', $4))
		ORDER BY `+order, viewerID, filter.Artist, filter.Genre, filter.Query)
	if err != nil {
		return nil, err
	}
	return scanCatalogVariants(rows)
}

// ListVariantsForWork lists every version of one work that viewerID
// may see (song_visible: the public ones, and the private ones of the
// person, their bands and their communities), public first — the work's
// "this song’s recordings and arrangements" page.
func (st *Store) ListVariantsForWork(ctx context.Context, workID, viewerID int64) ([]CatalogVariant, error) {
	rows, err := st.db.Query(ctx, `
		SELECT `+catalogVariantCols+`, coalesce(w.genre, ''), `+songAlbumSQL("s")+`
		FROM songs s JOIN works w ON w.id = s.work_id
		WHERE s.work_id = $1 AND song_visible(s.id, $2)
		ORDER BY s.public DESC, s.id`, workID, viewerID)
	if err != nil {
		return nil, err
	}
	return scanCatalogVariants(rows)
}

// catalogVariantCols are a CatalogVariant's columns, for songs s (what
// scanCatalogVariants reads before genre and album).
var catalogVariantCols = `s.id, s.work_id, s.title, s.artist, ` + songKindSQL("s") + `, s.public, s.version_name,
	s.owner_person_id, s.owner_band_id, s.owner_community_id, s.forked_from_id`

func scanCatalogVariants(rows pgx.Rows) ([]CatalogVariant, error) {
	defer rows.Close()
	var out []CatalogVariant
	for rows.Next() {
		var v CatalogVariant
		if err := rows.Scan(&v.VariantID, &v.WorkID, &v.Title, &v.Artist, &v.Visibility, &v.Public, &v.VersionName,
			&v.OwnerID, &v.OwnerBandID, &v.OwnerCommunityID, &v.ForkedFromID, &v.Genre, &v.Album); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CanEditVariant reports whether personID may change variantID in place
// (and revert its revisions): SongRightsFor's Edit — the person for their
// own song; a band's current members, core or guest; a community's
// members with edit_repertoire who see it; the site's admins for The Big
// Songbook's.
func (st *Store) CanEditVariant(ctx context.Context, personID, variantID int64) (bool, error) {
	r, err := st.SongRightsFor(ctx, personID, variantID)
	if err != nil {
		return false, err
	}
	return r.Edit, nil
}

// CanViewVariant reports whether personID may open variantID:
// SongRightsFor's See (song_visible).
func (st *Store) CanViewVariant(ctx context.Context, personID, variantID int64) (bool, error) {
	r, err := st.SongRightsFor(ctx, personID, variantID)
	if err != nil {
		return false, err
	}
	return r.See, nil
}

// CanCopyVariant reports whether personID may make a version of variantID
// for scope ("user", "band" or "community") targetID: SongRightsFor's
// CanVersionFor — what that owner may link (a band or a community, public
// songs and its own; a person, what they see), never a closed songbook's
// song.
func (st *Store) CanCopyVariant(ctx context.Context, personID, variantID int64, scope string, targetID int64) (bool, error) {
	r, err := st.SongRightsFor(ctx, personID, variantID)
	if err != nil {
		return false, err
	}
	return r.CanVersionFor(scopeOwner(scope, personID, targetID)), nil
}

// scopeOwner is ForkVariant's scope as an Owner.
func scopeOwner(scope string, personID, targetID int64) Owner {
	switch scope {
	case "band":
		return BandOwner(targetID)
	case "community":
		return CommunityOwner(targetID)
	}
	return PersonOwner(personID)
}

// GetVariant loads one variant's full catalog row by id (not scoped to a
// book/stem the way LoadSong is) — used by the fork/annotate/"all
// versions" endpoints, which address a variant by its row id.
func (st *Store) GetVariant(ctx context.Context, variantID int64) (*CatalogVariant, error) {
	var v CatalogVariant
	err := st.db.QueryRow(ctx, `
		SELECT `+catalogVariantCols+`
		FROM songs s WHERE s.id = $1`, variantID).
		Scan(&v.VariantID, &v.WorkID, &v.Title, &v.Artist, &v.Visibility, &v.Public, &v.VersionName,
			&v.OwnerID, &v.OwnerBandID, &v.OwnerCommunityID, &v.ForkedFromID)
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

// ForkVariant makes forkerID's version of variantID for scope "user" (the
// person's own), "band" (bandID's) or "community" (bandID, a community's):
// MakeVersion, in every songbook of that owner's holding the song as a
// link, else in its default songbook. Returns the new song's id.
func (st *Store) ForkVariant(ctx context.Context, variantID, forkerID int64, scope string, bandID int64) (int64, error) {
	if scope != "user" && scope != "band" && scope != "community" {
		return 0, fmt.Errorf("invalid fork scope %q", scope)
	}
	return st.MakeVersion(ctx, forkerID, variantID, scopeOwner(scope, forkerID, bandID), 0, "")
}

// CreateBandVariant creates a brand-new, from-scratch variant owned by
// bandID (owner_band_id = bandID, private) — the band-scoped
// sibling of store.CreateSong: same creation shape (title/artist header
// plus one empty [Verse] section to start from), just owned by the band. Before this method
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

	// hasBandPrivilege(ctx, tx, ...), not st.HasBandPrivilege — tx is
	// already open (see isCurrentBandMember's doc comment in bands.go).
	may, err := hasBandPrivilege(ctx, tx, bandID, creatorPersonID, BandPrivEditSongbook)
	if err != nil {
		return 0, err
	}
	if !may {
		return 0, fmt.Errorf("person %d may not add songs to band %d: %w", creatorPersonID, bandID, ErrForbidden)
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
		INSERT INTO songs (title, artist, body, work_id, owner_band_id, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6)
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
// communityID (owner_community_id = communityID, private) — the
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
		INSERT INTO songs (title, artist, body, work_id, owner_community_id, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6)
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
	return st.SaveSongEdit(ctx, SongSave{
		SongID: variantID, Title: title, Artist: artist, ReferenceURL: referenceURL, Body: body,
		ExpectVersion: expectVersion, ActorID: actorPersonID,
	})
}

// SongSave is one save of a song's chart: its new title, artist,
// reference and body, at ExpectVersion ("": whatever it is), by ActorID
// (0: unknown), with the edit's one-line Summary (the revision's).
type SongSave struct {
	SongID                            int64
	Title, Artist, ReferenceURL, Body string
	ExpectVersion                     string
	ActorID                           int64
	Summary                           string
}

// SaveSongEdit is SaveVariantByID with the edit's summary.
func (st *Store) SaveSongEdit(ctx context.Context, e SongSave) (string, error) {
	version, _, err := st.SaveSongEditRevision(ctx, e)
	return version, err
}

// SaveSongEditRevision is SaveSongEdit, with the revision it kept (the
// song before it: reverting it undoes the save).
func (st *Store) SaveSongEditRevision(ctx context.Context, e SongSave) (string, int64, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback(ctx)
	saved, err := saveSongTx(ctx, tx, e)
	if err != nil {
		return "", 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	return strconv.Itoa(saved.version), saved.revisionID, nil
}

// songSaveResult is what saveSongTx did: the song's new version, its
// body before, the revision kept.
type songSaveResult struct {
	version    int
	before     string
	revisionID int64
}

// saveSongTx writes e in tx: the song's state before is kept as a
// revision (with e's summary), its version moves on, its lead's sync
// follows. The caller commits.
func saveSongTx(ctx context.Context, tx pgx.Tx, e SongSave) (songSaveResult, error) {
	var out songSaveResult
	var curTitle, curArtist, curRef, curBody string
	var curVersion int
	err := tx.QueryRow(ctx, `
		SELECT title, artist, reference_url, body, version FROM songs
		WHERE id = $1 FOR UPDATE`, e.SongID).
		Scan(&curTitle, &curArtist, &curRef, &curBody, &curVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, fmt.Errorf("no such variant %d: %w", e.SongID, ErrNotFound)
	}
	if err != nil {
		return out, err
	}
	if e.ExpectVersion != "" && strconv.Itoa(curVersion) != e.ExpectVersion {
		return out, ErrConflict
	}

	var actor *int64
	if e.ActorID != 0 {
		actor = &e.ActorID
	}
	if out.revisionID, err = recordRevision(ctx, tx, "variant", strconv.FormatInt(e.SongID, 10), actor, VariantSnapshot{
		Title: curTitle, Artist: curArtist, ReferenceURL: curRef, Body: curBody,
	}, e.Summary); err != nil {
		return out, err
	}
	if err := setRevisionVersion(ctx, tx, out.revisionID, curVersion); err != nil {
		return out, err
	}

	if err := tx.QueryRow(ctx, `
		UPDATE songs SET title = $1, artist = $2, reference_url = $3, body = $4, version = version + 1
		WHERE id = $5
		RETURNING version`,
		e.Title, e.Artist, e.ReferenceURL, e.Body, e.SongID).Scan(&out.version); err != nil {
		return out, err
	}
	// A lead changed: the recordings' bars move with the song's.
	if _, err := shiftLeadSync(ctx, tx, e.SongID, curBody, e.Body); err != nil {
		return out, err
	}
	// Its key moved: the keys songbooks keep it in stay (entry_keys.go).
	if _, err := keepEntryKeys(ctx, tx, e.SongID, curBody, e.Body); err != nil {
		return out, err
	}
	out.before = curBody
	return out, nil
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
// GlobalVariantStub is just enough of a catalogue song to
// drive a batch re-fetch/redo tool (cmd/bandscampredo) — id/title/
// artist, nothing else.
type GlobalVariantStub struct {
	ID            int64
	Title, Artist string
}

// FindGlobalVariantByWorkID looks for an existing catalogue
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
		SELECT id FROM songs s WHERE s.work_id = $1 AND `+catalogueSQL("s")+` ORDER BY id LIMIT 1`, workID).Scan(&variantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return variantID, true, nil
}

// ListGlobalVariantsByBatchTimestamp lists every catalogue
// song whose created_at (truncated to the second) matches batchSecond
// — a bulk import lands every row in one INSERT within the same
// second, so this isolates one specific batch from the rest of the
// global catalog without needing a separate "which batch" column.
func (st *Store) ListGlobalVariantsByBatchTimestamp(ctx context.Context, batchSecond time.Time) ([]GlobalVariantStub, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, title, artist FROM songs
		WHERE `+catalogueSQL("songs")+` AND date_trunc('second', created_at) = date_trunc('second', $1::timestamptz)
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
