// Package store is the Postgres-backed repository of projects, songs and
// setlists. A "book" (the term predates this package's move to Postgres,
// kept because the HTTP layer and URLs still use it) is a projects row;
// book/stem pairs are resolved to project_id/stem lookups.
package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"app/internal/song"
)

type Store struct {
	db *pgxpool.Pool
}

// New connects to Postgres at connString and applies any pending
// migrations. The returned Store's Close must be called when done.
func New(ctx context.Context, connString string) (*Store, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", connString, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	return &Store{db: pool}, nil
}

func (st *Store) Close() {
	st.db.Close()
}

var ErrConflict = fmt.Errorf("song changed since it was loaded")
var ErrNotFound = fmt.Errorf("not found")

// ErrForbidden is returned by store methods that enforce a permission
// check themselves (e.g. ForkVariant's band-membership check) rather than
// leaving it entirely to the HTTP layer — used where the check needs to
// happen inside the same transaction as the write it's guarding.
var ErrForbidden = fmt.Errorf("forbidden")

func validName(s string) bool {
	return s != "" && !strings.ContainsAny(s, "/\\")
}

// ListBooks returns every project's slug, alphabetically.
func (st *Store) ListBooks() ([]string, error) {
	rows, err := st.db.Query(context.Background(), `SELECT slug FROM projects ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, rows.Err()
}

// ListSongs returns the stems of every song in book, alphabetically.
func (st *Store) ListSongs(book string) ([]string, error) {
	rows, err := st.db.Query(context.Background(), `
		SELECT s.stem FROM songs s JOIN projects p ON p.id = s.project_id
		WHERE p.slug = $1 ORDER BY s.stem`, book)
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

// CreateBook makes a new, empty project.
func (st *Store) CreateBook(name string) error {
	if !validName(name) {
		return fmt.Errorf("invalid book name: %q", name)
	}
	_, err := st.db.Exec(context.Background(),
		`INSERT INTO projects (slug, name) VALUES ($1, $1)`, name)
	return err
}

// DeleteBook removes an entire project and everything under it (songs,
// setlists, revisions — cascades via FK) — irreversible, so callers must
// only reach this from an explicit, confirmed user action.
func (st *Store) DeleteBook(book string) error {
	if !validName(book) {
		return fmt.Errorf("invalid book name: %q", book)
	}
	_, err := st.db.Exec(context.Background(), `DELETE FROM projects WHERE slug = $1`, book)
	return err
}

// ProjectID resolves a project (book) slug to its row id, or ErrNotFound.
// Exported wrapper around projectID for callers outside this package
// that need to resolve a book slug to attach new material to (e.g. the
// band repertoire page's "new original song" form, which lets a band
// pick an existing project for a from-scratch band variant to live
// under — see CreateBandVariant's doc comment).
func (st *Store) ProjectID(ctx context.Context, book string) (int64, error) {
	return st.projectID(ctx, book)
}

func (st *Store) projectID(ctx context.Context, book string) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `SELECT id FROM projects WHERE slug = $1`, book).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("no such project %q: %w", book, ErrNotFound)
	}
	return id, err
}

// CreateSong makes a new song (variant) with just a title/artist header
// and one empty [Verse] section to start from. Fails if the stem is
// already taken. creatorPersonID is recorded as created_by_person_id (0
// means "unknown/system", stored as NULL) — the variant itself stays
// visibility='global' (shared project content, same as every song this
// app has ever had), not owner_person_id-scoped to the creator; only a
// fork is personally owned. A work is matched-or-created from
// title/artist per the "work vs variant" model (internal/store/catalog.go).
func (st *Store) CreateSong(book, stem, title, artist string, creatorPersonID int64) error {
	if !validName(stem) {
		return fmt.Errorf("invalid stem: %q", stem)
	}
	ctx := context.Background()
	pid, err := st.projectID(ctx, book)
	if err != nil {
		return err
	}
	var exists bool
	if err := st.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM songs WHERE project_id = $1 AND stem = $2)`, pid, stem).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("a song already exists at %s/%s", book, stem)
	}

	workID, err := st.FindOrCreateWork(ctx, title, artist)
	if err != nil {
		return fmt.Errorf("resolving work for %q/%q: %w", title, artist, err)
	}

	var b strings.Builder
	b.WriteString("title: " + title + "\n")
	if artist != "" {
		b.WriteString("artist: " + artist + "\n")
	}
	b.WriteString("\n[Verse]\n")
	body := b.String()

	var creator *int64
	if creatorPersonID != 0 {
		creator = &creatorPersonID
	}
	_, err = st.db.Exec(ctx,
		`INSERT INTO songs (project_id, stem, title, artist, reference_url, body, work_id, created_by_person_id)
		 VALUES ($1, $2, $3, $4, '', $5, $6, $7)`,
		pid, stem, title, artist, body, workID, creator)
	return err
}

// RedoSong resets an existing song's body back to the same blank
// title/[Verse]-only template CreateSong starts a brand-new song from —
// "redo this song from scratch": same identity (book+stem, same row, same
// revision history), but every bit of body content (structure, lyrics,
// chords, and the title/artist header along with it, since a from-scratch
// redo really does start the wizard over) is discarded and replaced.
// Same optimistic-concurrency guard as SaveSong/SaveRaw — the caller
// should pass the version it last loaded so a redo started against stale
// data is rejected with ErrConflict rather than clobbering a save that
// happened in between.
func (st *Store) RedoSong(book, stem, expectVersion string, actorPersonID int64) (string, string, error) {
	body := "title: " + stem + "\n\n[Verse]\n"
	version, err := st.saveBody(book, stem, body, expectVersion, actorPersonID)
	return body, version, err
}

// DeleteSong removes one song.
func (st *Store) DeleteSong(book, stem string) error {
	ctx := context.Background()
	pid, err := st.projectID(ctx, book)
	if err != nil {
		return err
	}
	tag, err := st.db.Exec(ctx, `DELETE FROM songs WHERE project_id = $1 AND stem = $2`, pid, stem)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such song %s/%s: %w", book, stem, ErrNotFound)
	}
	return nil
}

// LoadSong parses one song's body. The returned string is the song's
// version, an opaque token for optimistic-concurrency checks on save.
func (st *Store) LoadSong(book, stem string) (*song.Song, string, error) {
	body, version, err := st.loadBody(book, stem)
	if err != nil {
		return nil, "", err
	}
	return song.Parse(body), version, nil
}

func (st *Store) loadBody(book, stem string) (string, string, error) {
	var body string
	var version int
	err := st.db.QueryRow(context.Background(), `
		SELECT s.body, s.version FROM songs s JOIN projects p ON p.id = s.project_id
		WHERE p.slug = $1 AND s.stem = $2`, book, stem).Scan(&body, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("no such song %s/%s: %w", book, stem, ErrNotFound)
	}
	if err != nil {
		return "", "", err
	}
	return body, strconv.Itoa(version), nil
}

// SaveSong writes s's serialized form as the song's body. If expectVersion
// is non-empty, the save is rejected with ErrConflict when the row's
// current version doesn't match. actorPersonID (0 = unknown) is recorded
// as the revision's actor — see RecordRevision.
func (st *Store) SaveSong(book, stem string, s *song.Song, expectVersion string, actorPersonID int64) (string, error) {
	return st.saveBody(book, stem, song.Write(s), expectVersion, actorPersonID)
}

// LoadRaw returns a song's body verbatim, for the raw-text editor.
func (st *Store) LoadRaw(book, stem string) (string, string, error) {
	return st.loadBody(book, stem)
}

// SaveRaw writes text verbatim as the song's body, with the same
// optimistic-concurrency guard as SaveSong.
func (st *Store) SaveRaw(book, stem, text, expectVersion string, actorPersonID int64) (string, error) {
	return st.saveBody(book, stem, text, expectVersion, actorPersonID)
}

// saveBody is every variant-body write's common path: every edit to a
// variant's body/grid/lyrics/header fields goes through here, and every
// one of them records a revision (the pre-edit snapshot) in the same
// transaction as the write, via RecordRevision — this is phase 1's wiring
// of the generic revision/undo mechanism into the existing save path (see
// internal/store/revisions.go). The SELECT ... FOR UPDATE + version check
// + INSERT + UPDATE all happen in one transaction, so a concurrent save
// can't interleave with the revision snapshot.
func (st *Store) saveBody(book, stem, body, expectVersion string, actorPersonID int64) (string, error) {
	ctx := context.Background()
	pid, err := st.projectID(ctx, book)
	if err != nil {
		return "", err
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var id int64
	var curBody, curTitle, curArtist, curRef string
	var curVersion int
	err = tx.QueryRow(ctx, `
		SELECT id, body, title, artist, reference_url, version FROM songs
		WHERE project_id = $1 AND stem = $2 FOR UPDATE`, pid, stem).
		Scan(&id, &curBody, &curTitle, &curArtist, &curRef, &curVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no such song %s/%s: %w", book, stem, ErrNotFound)
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
	if err := st.RecordRevision(ctx, tx, "variant", strconv.FormatInt(id, 10), actor, VariantSnapshot{
		Title: curTitle, Artist: curArtist, ReferenceURL: curRef, Body: curBody,
	}); err != nil {
		return "", err
	}

	parsed := song.Parse(body)
	title, artist, ref := parsed.Get("title"), parsed.Get("artist"), parsed.Get("reference")

	var newVersion int
	if err := tx.QueryRow(ctx, `
		UPDATE songs SET body = $1, title = $2, artist = $3, reference_url = $4, version = version + 1
		WHERE id = $5
		RETURNING version`,
		body, title, artist, ref, id).Scan(&newVersion); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return strconv.Itoa(newVersion), nil
}

// ImportSong inserts a song row directly (used by the import tool and by
// zip import, where the stem is known not to collide in a fresh project).
// Imported songs are visibility='global' (the column default) with no
// owner — this is exactly the catalog's bulk-loaded, shared content. A
// work is matched-or-created from title/artist same as CreateSong.
func (st *Store) ImportSong(book, stem, body string) error {
	ctx := context.Background()
	pid, err := st.projectID(ctx, book)
	if err != nil {
		return err
	}
	parsed := song.Parse(body)
	title, artist, ref := parsed.Get("title"), parsed.Get("artist"), parsed.Get("reference")
	workID, err := st.FindOrCreateWork(ctx, title, artist)
	if err != nil {
		return fmt.Errorf("resolving work for %q/%q: %w", title, artist, err)
	}
	_, err = st.db.Exec(ctx, `
		INSERT INTO songs (project_id, stem, title, artist, reference_url, body, work_id) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (project_id, stem) DO NOTHING`,
		pid, stem, title, artist, ref, body, workID)
	return err
}
