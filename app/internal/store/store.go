// Package store is the Postgres-backed repository of songs, songbooks and
// setlists. A song variant is addressed by its own id — there is no
// "project"/"book" container any more (see migration 0041's doc comment
// for the removal and the real-data decisions that came with it).
package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// ErrEmailTaken is returned by SetEmail when the address already belongs
// to a different account (people.email has a UNIQUE constraint — see
// migration 0025).
var ErrEmailTaken = fmt.Errorf("email already in use by another account")

// ErrUsernameTaken is returned by CreatePersonAndAcceptInvite when the
// chosen username already belongs to another account (people.username
// has a UNIQUE constraint — see migration 0001).
var ErrUsernameTaken = fmt.Errorf("username already in use by another account")

func validName(s string) bool {
	return s != "" && !strings.ContainsAny(s, "/\\")
}

// LoadVariant parses one variant's body by id, INCLUDING every section's
// "score[label]:" voices (song.Section.Scores) — those are plain text in
// the body now, resolved into real Riffs by tab.ParseVoice at render
// time (see attachTabRiffs/attachDrumRiff, handlers_pages.go), not a
// separate database lookup this function would otherwise need to offer
// as its own step. The returned string is the variant's version, an
// opaque token for optimistic-concurrency checks on save.
func (st *Store) LoadVariant(variantID int64) (*song.Song, string, error) {
	body, version, err := st.LoadVariantRaw(variantID)
	if err != nil {
		return nil, "", err
	}
	return song.Parse(body), version, nil
}

// LoadVariantRaw returns a variant's body verbatim, for the raw-text
// editor, plus its version.
func (st *Store) LoadVariantRaw(variantID int64) (string, string, error) {
	var body string
	var version int
	err := st.db.QueryRow(context.Background(),
		`SELECT body, version FROM songs WHERE id = $1`, variantID).Scan(&body, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	if err != nil {
		return "", "", err
	}
	return body, strconv.Itoa(version), nil
}

// SaveVariant writes s's serialized form as variantID's body, via
// SaveVariantByID (see catalog.go) — this wrapper just does the
// song.Write/parse plumbing LoadVariant's counterpart needs, same
// division SaveSong/SaveRaw used to have.
func (st *Store) SaveVariant(variantID int64, s *song.Song, expectVersion string, actorPersonID int64) (string, error) {
	body := song.Write(s)
	parsed := song.Parse(body)
	return st.SaveVariantByID(context.Background(), variantID, parsed.Get("title"), parsed.Get("artist"), parsed.Get("reference"), body, expectVersion, actorPersonID)
}

// SaveVariantRaw writes text verbatim as variantID's body, with the same
// optimistic-concurrency guard as SaveVariant.
func (st *Store) SaveVariantRaw(variantID int64, text, expectVersion string, actorPersonID int64) (string, error) {
	parsed := song.Parse(text)
	return st.SaveVariantByID(context.Background(), variantID, parsed.Get("title"), parsed.Get("artist"), parsed.Get("reference"), text, expectVersion, actorPersonID)
}

// RedoVariant resets an existing variant's body back to a blank
// title/[Verse]-only template — "redo this song from scratch": same
// identity (same row, same revision history), but every bit of body
// content is discarded and replaced. Same optimistic-concurrency guard as
// SaveVariant.
func (st *Store) RedoVariant(variantID int64, title, expectVersion string, actorPersonID int64) (string, string, error) {
	body := "title: " + title + "\n\n[Verse]\n"
	version, err := st.SaveVariantRaw(variantID, body, expectVersion, actorPersonID)
	return body, version, err
}

// DeleteVariant removes one variant.
func (st *Store) DeleteVariant(variantID int64) error {
	tag, err := st.db.Exec(context.Background(), `DELETE FROM songs WHERE id = $1`, variantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	return nil
}

// OwnerKind selects who a brand-new variant belongs to at creation —
// "private, band-private, or public catalogue" per the songs-management
// brief.
type OwnerKind string

const (
	OwnerCatalog   OwnerKind = "catalog"   // public: visibility='global', no owner
	OwnerPerson    OwnerKind = "person"    // private: visibility='user', owner_person_id
	OwnerBand      OwnerKind = "band"      // band-private: visibility='band', owner_band_id
	OwnerCommunity OwnerKind = "community" // community-private: visibility='community', owner_community_id
)

// CreateVariant makes a brand-new, from-scratch variant (title/artist
// header plus one empty [Verse] section) owned per ownerKind/ownerID —
// the single creation entry point replacing the old per-project
// CreateSong plus the per-scope CreateBandVariant/CreateCommunityVariant
// (community creation stays in its own file, community_catalog.go,
// since it also needs an atomic "add to my own songbook" step
// CreateVariant has no reason to know about). A new variant is also,
// atomically, added to the owner's default songbook (OwnerPerson/
// OwnerBand) — "a variant that isn't in its owner's own songbook doesn't
// make sense."
func (st *Store) CreateVariant(ctx context.Context, ownerKind OwnerKind, ownerID int64, title, artist string, creatorPersonID int64) (int64, error) {
	// Resolved before the transaction, same fix CreateBandVariant/
	// CreateCommunityVariant already needed: GetDefaultSongbook isn't
	// tx-scoped and may itself need to INSERT a songbook row. Without
	// this, the raw "... AND is_default" SELECT further down 404'd with
	// a bare "no rows in result set" for anyone whose default songbook
	// hadn't been created yet — confirmed live for a brand-new person's
	// very first personal song (cmd/songimport creating a fresh person's
	// first variant hit this immediately; the band/community branches
	// were already fixed, person never was).
	var defaultSbID int64
	if ownerKind == OwnerPerson || ownerKind == OwnerBand || ownerKind == OwnerCommunity {
		kind := "person"
		switch ownerKind {
		case OwnerBand:
			kind = "band"
		case OwnerCommunity:
			kind = "community"
		}
		var err error
		defaultSbID, err = st.GetDefaultSongbook(ctx, kind, ownerID)
		if err != nil {
			return 0, err
		}
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if ownerKind == OwnerBand {
		isMember, err := isCurrentBandMember(ctx, tx, ownerID, creatorPersonID)
		if err != nil {
			return 0, err
		}
		if !isMember {
			return 0, fmt.Errorf("person %d is not a current member of band %d: %w", creatorPersonID, ownerID, ErrForbidden)
		}
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
	switch ownerKind {
	case OwnerPerson:
		err = tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, body, work_id, visibility, owner_person_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, 'user', $5, $6) RETURNING id`,
			title, artist, b.String(), workID, ownerID, creator).Scan(&newID)
	case OwnerBand:
		err = tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, body, work_id, visibility, owner_band_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, 'band', $5, $6) RETURNING id`,
			title, artist, b.String(), workID, ownerID, creator).Scan(&newID)
	case OwnerCommunity:
		err = tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, body, work_id, visibility, owner_community_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, 'community', $5, $6) RETURNING id`,
			title, artist, b.String(), workID, ownerID, creator).Scan(&newID)
	default: // OwnerCatalog
		err = tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, body, work_id, created_by_person_id)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			title, artist, b.String(), workID, creator).Scan(&newID)
	}
	if err != nil {
		return 0, err
	}

	if ownerKind == OwnerPerson || ownerKind == OwnerBand || ownerKind == OwnerCommunity {
		if _, err := tx.Exec(ctx, `
			INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)`,
			defaultSbID, newID, creator); err != nil {
			return 0, err
		}
	}

	return newID, tx.Commit(ctx)
}

// ImportVariant inserts a variant row directly, visibility='global' (the
// column default) with no owner — used by the bulk-import tool. A work
// is matched-or-created from title/artist same as CreateVariant.
func (st *Store) ImportVariant(ctx context.Context, body string) error {
	parsed := song.Parse(body)
	title, artist, ref := parsed.Get("title"), parsed.Get("artist"), parsed.Get("reference")
	workID, err := st.FindOrCreateWork(ctx, title, artist)
	if err != nil {
		return fmt.Errorf("resolving work for %q/%q: %w", title, artist, err)
	}
	_, err = st.db.Exec(ctx, `
		INSERT INTO songs (title, artist, reference_url, body, work_id) VALUES ($1, $2, $3, $4, $5)`,
		title, artist, ref, body, workID)
	return err
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505), any constraint.
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}

// constraintViolated reports whether err is a unique-violation on
// specifically the named constraint — for a caller that needs to tell
// WHICH of several unique columns conflicted (e.g. username vs email on
// the same INSERT), not just that one of them did.
func constraintViolated(err error, name string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505" && pgErr.ConstraintName == name
}
