package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/i18n"
	"app/internal/song"
	"app/internal/songdiff"
)

// The catalogue: open edits (docs/ownership-model.md, "The catalogue:
// open edits"; migration 0125). Anyone signed in edits The Big
// Songbook's songs (SongRights.Contribute). Nothing is lost: every edit
// is a revision with its one-line summary, which its author (while it's
// the latest), a trusted editor or the staff revert in one click. The
// reference recording is the anchor: the HTTP layer weighs each edit
// against it (httpapi/catalogue_edits.go: the "follows the recording"
// check before and after, the structure, the lead-in, the Guitar Pro
// sync, the key) and either applies it at once (ApplyCatalogueEdit) or
// holds it for review (HoldCatalogueEdit): the song stays as it was for
// everyone until a reviewer approves it (ApproveCatalogueEdit) or
// rejects it with a note (RejectCatalogueEdit). Reviewers are the staff
// (the site's admins) and trusted editors: CatalogueTrustedEdits
// accepted edits or more. A newcomer's first CatalogueNewcomerEdits edits
// wait for review whatever they change. Changing a song's recording
// makes a new catalogue song for it (CreateRecordingVersion).

// The thresholds.
const (
	// CatalogueNewcomerEdits: until this many of a person's edits are
	// accepted, each one waits for review.
	CatalogueNewcomerEdits = 3
	// CatalogueTrustedEdits: this many accepted edits make a trusted
	// editor, who reviews the others' and reverts any edit.
	CatalogueTrustedEdits = 10
	// CatalogueDailyLimit: someone not trusted yet has this many edits
	// applied in a day (24 hours); the next ones wait for a reviewer.
	CatalogueDailyLimit = 10
)

// acceptedEditSQL picks an accepted edit (e): applied or approved, and
// not reverted within 7 days of it — what counts towards trust.
const acceptedEditSQL = `e.status IN ('applied', 'approved') AND (e.reverted_at IS NULL OR e.reverted_at > e.created_at + interval '7 days')`

// The statuses of a catalogue edit.
const (
	EditApplied    = "applied"
	EditPending    = "pending"
	EditApproved   = "approved"
	EditRejected   = "rejected"
	EditSuperseded = "superseded"
)

// Notification kinds.
const (
	// NotificationCatalogueReview: catalogue edits or reports wait (one
	// unread per reviewer while they do: batched, not one per edit).
	NotificationCatalogueReview = "catalogue_review"
	// NotificationCatalogueEditDecided: an author's edit was approved or
	// rejected.
	NotificationCatalogueEditDecided = "catalogue_edit_decided"
)

// CatalogueReviewLink is the review queue.
const CatalogueReviewLink = "/big-songbook/review"

// CatalogueEditLink is one edit's page.
func CatalogueEditLink(id int64) string { return fmt.Sprintf("/big-songbook/edits/%d", id) }

// CatalogueStanding is where a person stands with the catalogue.
type CatalogueStanding struct {
	Accepted int  // edits applied or approved
	Staff    bool // a site admin
	Trusted  bool // Accepted >= CatalogueTrustedEdits
	Banned   bool // kept from editing it by its staff
}

// Newcomer: one of their first edits; applied like anyone's, and
// flagged for the reviewers (docs/catalogue-edit-review.md §4.5).
func (s CatalogueStanding) Newcomer() bool { return !s.Staff && s.Accepted < CatalogueNewcomerEdits }

// Reviewer: they see the queue, approve and reject, revert any edit.
func (s CatalogueStanding) Reviewer() bool { return !s.Banned && (s.Staff || s.Trusted) }

// CatalogueStandingOf is personID's standing.
func (st *Store) CatalogueStandingOf(ctx context.Context, personID int64) (CatalogueStanding, error) {
	var s CatalogueStanding
	err := st.db.QueryRow(ctx, `
		SELECT p.is_admin, p.catalogue_banned_at IS NOT NULL,
		       (SELECT count(*) FROM catalogue_edits e WHERE e.author_id = p.id AND `+acceptedEditSQL+`)
		FROM people p WHERE p.id = $1`, personID).Scan(&s.Staff, &s.Banned, &s.Accepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, fmt.Errorf("no such person %d: %w", personID, ErrNotFound)
	}
	s.Trusted = s.Accepted >= CatalogueTrustedEdits
	return s, err
}

// CatalogueEdit is one edit of a catalogue song by someone who isn't its
// staff (catalogue_edits).
type CatalogueEdit struct {
	ID           int64
	SongID       int64
	SongTitle    string // the song's, now
	SongArtist   string
	SongVersion  int // the song's version now
	AuthorID     int64
	AuthorName   string
	Status       string
	BaseVersion  int
	BaseBody     string
	Title        string
	Artist       string
	ReferenceURL string
	Body         string
	Summary      string
	Reasons      []string
	FitBefore    string
	FitAfter     string
	// The fits' reasons (songimport.FitReason, JSON), nil for none.
	FitBeforeDetail json.RawMessage
	FitAfterDetail  json.RawMessage
	RevisionID      *int64
	DecidedBy       *int64
	DecidedByName   string
	DecidedAt       *time.Time
	Note            string
	CreatedAt       time.Time
	// Patrol (migration 0141): an applied edit seen by a reviewer (or
	// made by a trusted editor), or reverted.
	PatrolledAt     *time.Time
	PatrolledByName string
	RevertedAt      *time.Time
	RevertedByName  string
}

const catalogueEditCols = `e.id, e.song_id, s.title, s.artist, s.version, coalesce(e.author_id, 0), coalesce(a.display_name, ''),
	e.status, e.base_version, e.base_body, e.title, e.artist, e.reference_url, e.body, e.summary, e.reasons,
	e.fit_before, e.fit_after, e.fit_before_detail, e.fit_after_detail, e.revision_id,
	e.decided_by, coalesce(d.display_name, ''), e.decided_at, e.note, e.created_at,
	e.patrolled_at, coalesce(pb.display_name, ''), e.reverted_at, coalesce(rb.display_name, '')`

const catalogueEditFrom = ` FROM catalogue_edits e JOIN songs s ON s.id = e.song_id
	LEFT JOIN people a ON a.id = e.author_id LEFT JOIN people d ON d.id = e.decided_by
	LEFT JOIN people pb ON pb.id = e.patrolled_by LEFT JOIN people rb ON rb.id = e.reverted_by`

func scanCatalogueEdit(row pgx.Row) (*CatalogueEdit, error) {
	var e CatalogueEdit
	var before, after []byte
	err := row.Scan(&e.ID, &e.SongID, &e.SongTitle, &e.SongArtist, &e.SongVersion, &e.AuthorID, &e.AuthorName,
		&e.Status, &e.BaseVersion, &e.BaseBody, &e.Title, &e.Artist, &e.ReferenceURL, &e.Body, &e.Summary, &e.Reasons,
		&e.FitBefore, &e.FitAfter, &before, &after, &e.RevisionID,
		&e.DecidedBy, &e.DecidedByName, &e.DecidedAt, &e.Note, &e.CreatedAt,
		&e.PatrolledAt, &e.PatrolledByName, &e.RevertedAt, &e.RevertedByName)
	if err != nil {
		return nil, err
	}
	if len(before) > 0 {
		e.FitBeforeDetail = before
	}
	if len(after) > 0 {
		e.FitAfterDetail = after
	}
	return &e, nil
}

// CatalogueEditByID is one edit.
func (st *Store) CatalogueEditByID(ctx context.Context, id int64) (*CatalogueEdit, error) {
	e, err := scanCatalogueEdit(st.db.QueryRow(ctx, `SELECT `+catalogueEditCols+catalogueEditFrom+` WHERE e.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such catalogue edit %d: %w", id, ErrNotFound)
	}
	return e, err
}

func (st *Store) catalogueEdits(ctx context.Context, where string, args ...any) ([]CatalogueEdit, error) {
	rows, err := st.db.Query(ctx, `SELECT `+catalogueEditCols+catalogueEditFrom+` WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CatalogueEdit
	for rows.Next() {
		e, err := scanCatalogueEdit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// PendingCatalogueEdits is the review queue's edits, oldest first.
func (st *Store) PendingCatalogueEdits(ctx context.Context) ([]CatalogueEdit, error) {
	return st.catalogueEdits(ctx, `e.status = 'pending' ORDER BY e.created_at, e.id`)
}

// PendingCatalogueEditsOfSong is songID's edits waiting for a reviewer
// (its suggestions), oldest first.
func (st *Store) PendingCatalogueEditsOfSong(ctx context.Context, songID int64) ([]CatalogueEdit, error) {
	return st.catalogueEdits(ctx, `e.status = 'pending' AND e.song_id = $1 ORDER BY e.created_at, e.id`, songID)
}

// PendingCatalogueEditOf is personID's edit of songID waiting for
// review, nil when none.
func (st *Store) PendingCatalogueEditOf(ctx context.Context, personID, songID int64) (*CatalogueEdit, error) {
	es, err := st.catalogueEdits(ctx, `e.status = 'pending' AND e.author_id = $1 AND e.song_id = $2`, personID, songID)
	if err != nil || len(es) == 0 {
		return nil, err
	}
	return &es[0], nil
}

// CatalogueEditInput is an edit as the HTTP layer weighed it.
type CatalogueEditInput struct {
	SongID                            int64
	AuthorID                          int64
	BaseVersion                       int
	BaseBody                          string
	Title, Artist, ReferenceURL, Body string
	Summary                           string
	Reasons                           []string // why it waits; on an applied edit, why it's worth a look
	FitBefore, FitAfter               string
	FitBeforeDetail, FitAfterDetail   any
	// Patrolled: applied and seen as made (its author is trusted, and
	// nothing in it is worth a look); otherwise it's listed in "Recent
	// edits to check".
	Patrolled bool
}

func jsonOrNil(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return nil, err
	}
	return b, nil
}

func insertCatalogueEdit(ctx context.Context, tx pgx.Tx, in CatalogueEditInput, status string, revisionID *int64) (int64, error) {
	before, err := jsonOrNil(in.FitBeforeDetail)
	if err != nil {
		return 0, err
	}
	after, err := jsonOrNil(in.FitAfterDetail)
	if err != nil {
		return 0, err
	}
	reasons := in.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	var author *int64
	if in.AuthorID != 0 {
		author = &in.AuthorID
	}
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO catalogue_edits (song_id, author_id, status, base_version, base_body, title, artist, reference_url, body,
		                             summary, reasons, fit_before, fit_after, fit_before_detail, fit_after_detail, revision_id,
		                             patrolled_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
		        CASE WHEN $17::boolean THEN now() END) RETURNING id`,
		in.SongID, author, status, in.BaseVersion, in.BaseBody, in.Title, in.Artist, in.ReferenceURL, in.Body,
		in.Summary, reasons, in.FitBefore, in.FitAfter, before, after, revisionID, in.Patrolled && status == EditApplied).Scan(&id)
	return id, err
}

// ApplyCatalogueEdit applies in at once: the song changes (a revision
// kept, with its summary), the edit is logged as applied (it counts
// towards its author's trust), and the author's edit of the song that
// waited for review, if any, is superseded. ErrConflict when the song
// isn't at in.BaseVersion any more.
func (st *Store) ApplyCatalogueEdit(ctx context.Context, in CatalogueEditInput) (string, error) {
	version, _, err := st.ApplyCatalogueEditRevision(ctx, in)
	return version, err
}

// ApplyCatalogueEditRevision is ApplyCatalogueEdit, with the revision it
// kept (the song before it: its author's Undo reverts it).
func (st *Store) ApplyCatalogueEditRevision(ctx context.Context, in CatalogueEditInput) (string, int64, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback(ctx)
	saved, err := saveSongTx(ctx, tx, SongSave{
		SongID: in.SongID, Title: in.Title, Artist: in.Artist, ReferenceURL: in.ReferenceURL, Body: in.Body,
		ExpectVersion: strconv.Itoa(in.BaseVersion), ActorID: in.AuthorID, Summary: in.Summary,
	})
	if err != nil {
		return "", 0, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE catalogue_edits SET status = 'superseded' WHERE song_id = $1 AND author_id = $2 AND status = 'pending'`,
		in.SongID, in.AuthorID); err != nil {
		return "", 0, err
	}
	if _, err := insertCatalogueEdit(ctx, tx, in, EditApplied, &saved.revisionID); err != nil {
		return "", 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	// Not seen yet: the reviewers have recent edits to check.
	if !in.Patrolled {
		st.tellCatalogueReviewers(ctx, in.AuthorID)
	}
	return strconv.Itoa(saved.version), saved.revisionID, nil
}

// HoldCatalogueEdit keeps in for review: the song stays as it is; the
// author's earlier edit of it that waited is superseded; the reviewers
// are told (once while the queue has items). Returns the edit's id.
func (st *Store) HoldCatalogueEdit(ctx context.Context, in CatalogueEditInput) (int64, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE catalogue_edits SET status = 'superseded' WHERE song_id = $1 AND author_id = $2 AND status = 'pending'`,
		in.SongID, in.AuthorID); err != nil {
		return 0, err
	}
	id, err := insertCatalogueEdit(ctx, tx, in, EditPending, nil)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	st.tellCatalogueReviewers(ctx, in.AuthorID)
	return id, nil
}

// ErrNotReviewer: the person may not review catalogue edits (not its
// staff, not a trusted editor; or the edit is their own).
var ErrNotReviewer = fmt.Errorf("only the catalogue's staff and trusted editors review its edits, never their own: %w", ErrForbidden)

// ErrEditDecided: the edit was approved, rejected or superseded already.
var ErrEditDecided = errors.New("this edit was decided already")

func (st *Store) mayReview(ctx context.Context, personID int64, e *CatalogueEdit) error {
	s, err := st.CatalogueStandingOf(ctx, personID)
	if err != nil {
		return err
	}
	if !s.Reviewer() || (e != nil && e.AuthorID == personID && !s.Staff) {
		return ErrNotReviewer
	}
	return nil
}

// MayReviewCatalogue reports whether personID reviews catalogue edits.
func (st *Store) MayReviewCatalogue(ctx context.Context, personID int64) (bool, error) {
	err := st.mayReview(ctx, personID, nil)
	if errors.Is(err, ErrNotReviewer) {
		return false, nil
	}
	return err == nil, err
}

// ApproveCatalogueEdit makes edit id current, as its author's edit (its
// summary on the revision): over the song as it is now when it changed
// since the author's base (songdiff.Merge; ErrConflict when a part the
// edit changes changed since too: it is rejected then, and its author
// edits again). reviewerID: the staff or a trusted editor, not its
// author. The author is told.
func (st *Store) ApproveCatalogueEdit(ctx context.Context, id, reviewerID int64, note string) error {
	e, err := st.CatalogueEditByID(ctx, id)
	if err != nil {
		return err
	}
	if err := st.mayReview(ctx, reviewerID, e); err != nil {
		return err
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status, current string
	var version int
	if err := tx.QueryRow(ctx, `
		SELECT e.status, s.body, s.version FROM catalogue_edits e JOIN songs s ON s.id = e.song_id
		WHERE e.id = $1 FOR UPDATE OF e, s`, id).Scan(&status, &current, &version); err != nil {
		return err
	}
	if status != EditPending {
		return ErrEditDecided
	}
	body := e.Body
	if version != e.BaseVersion && current != e.BaseBody {
		parts := songdiff.Plan(e.BaseBody, current, e.Body)
		for _, p := range parts {
			if p.Conflict {
				return ErrConflict
			}
		}
		body = songdiff.MergeAll(e.BaseBody, current, e.Body)
	}
	parsed := song.Parse(body)
	saved, err := saveSongTx(ctx, tx, SongSave{
		SongID: e.SongID, Title: orElse(parsed.Get("title"), e.Title), Artist: parsed.Get("artist"),
		ReferenceURL: parsed.Get("reference"), Body: body, ExpectVersion: strconv.Itoa(version),
		ActorID: e.AuthorID, Summary: e.Summary,
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE catalogue_edits SET status = 'approved', revision_id = $2, decided_by = $3, decided_at = now(), note = $4
		WHERE id = $1`, id, saved.revisionID, reviewerID, strings.TrimSpace(note)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	st.tellAuthor(ctx, e, true)
	st.queueDrained(ctx)
	return nil
}

func orElse(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// RejectCatalogueEdit turns edit id down with a note for its author, who
// is told; the song stays as it is.
func (st *Store) RejectCatalogueEdit(ctx context.Context, id, reviewerID int64, note string) error {
	e, err := st.CatalogueEditByID(ctx, id)
	if err != nil {
		return err
	}
	if err := st.mayReview(ctx, reviewerID, e); err != nil {
		return err
	}
	tag, err := st.db.Exec(ctx, `
		UPDATE catalogue_edits SET status = 'rejected', decided_by = $2, decided_at = now(), note = $3
		WHERE id = $1 AND status = 'pending'`, id, reviewerID, strings.TrimSpace(note))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrEditDecided
	}
	e.Note = strings.TrimSpace(note)
	st.tellAuthor(ctx, e, false)
	st.queueDrained(ctx)
	return nil
}

// tellAuthor tells an edit's author it was approved or rejected. A
// failure is logged, never the decision's.
func (st *Store) tellAuthor(ctx context.Context, e *CatalogueEdit, approved bool) {
	if e.AuthorID == 0 {
		return
	}
	lang, _ := st.PersonLocale(ctx, e.AuthorID)
	if lang == "" {
		lang = "en"
	}
	body := i18n.T(lang, "Your edit of %s was approved: everyone has it now.", e.SongTitle)
	if !approved {
		body = i18n.T(lang, "Your edit of %s wasn’t taken.", e.SongTitle)
		if e.Note != "" {
			body += " " + i18n.T(lang, "The reviewer’s note: %s", e.Note)
		}
	}
	if _, err := st.CreateNotification(ctx, e.AuthorID, NotificationCatalogueEditDecided, body, CatalogueEditLink(e.ID)); err != nil {
		log.Printf("catalogue edit %d: telling its author: %v", e.ID, err)
	}
}

// CatalogueReviewers is who reviews catalogue edits: the staff and the
// trusted editors, none kept from editing.
func (st *Store) CatalogueReviewers(ctx context.Context) ([]int64, error) {
	rows, err := st.db.Query(ctx, `
		SELECT p.id FROM people p
		WHERE p.catalogue_banned_at IS NULL
		  AND (p.is_admin OR (SELECT count(*) FROM catalogue_edits e
		                      WHERE e.author_id = p.id AND `+acceptedEditSQL+`) >= $1)
		ORDER BY p.id`, CatalogueTrustedEdits)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// tellCatalogueReviewers tells the reviewers, but exceptID, that the
// queue has items: one unread notification each while it does (batched:
// a reviewer with one unread already isn't told again). A failure is
// logged, never the edit's.
func (st *Store) tellCatalogueReviewers(ctx context.Context, exceptID int64) {
	people, err := st.CatalogueReviewers(ctx)
	if err != nil {
		log.Printf("catalogue review: listing reviewers: %v", err)
		return
	}
	for _, p := range people {
		if p == exceptID {
			continue
		}
		var told bool
		if err := st.db.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM notifications WHERE person_id = $1 AND kind = $2 AND read_at IS NULL)`,
			p, NotificationCatalogueReview).Scan(&told); err != nil || told {
			continue
		}
		lang, _ := st.PersonLocale(ctx, p)
		if lang == "" {
			lang = "en"
		}
		if _, err := st.CreateNotification(ctx, p, NotificationCatalogueReview,
			i18n.T(lang, "Edits or reports of The Big Songbook’s songs wait for review"), CatalogueReviewLink); err != nil {
			log.Printf("catalogue review: telling %d: %v", p, err)
		}
	}
}

// queueDrained marks the reviewers' "wait for review" read once nothing
// waits any more.
func (st *Store) queueDrained(ctx context.Context) {
	if n, err := st.CatalogueQueueCount(ctx); err != nil || n > 0 {
		return
	}
	if _, err := st.db.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE kind = $1 AND read_at IS NULL`,
		NotificationCatalogueReview); err != nil {
		log.Printf("catalogue review: queue drained: %v", err)
	}
}

// SeenCatalogueQueue marks personID's "wait for review" read: they
// opened the queue.
func (st *Store) SeenCatalogueQueue(ctx context.Context, personID int64) error {
	_, err := st.db.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE person_id = $1 AND kind = $2 AND read_at IS NULL`,
		personID, NotificationCatalogueReview)
	return err
}

// CatalogueQueueCount is how many edits to check, edits waiting and
// reports there are.
func (st *Store) CatalogueQueueCount(ctx context.Context) (int, error) {
	var n int
	err := st.db.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM catalogue_edits WHERE status = 'pending')
		     + (SELECT count(*) FROM catalogue_edits WHERE `+toCheckSQL+`)
		     + (SELECT count(*) FROM catalogue_reports WHERE handled_at IS NULL)`).Scan(&n)
	return n, err
}

// toCheckSQL picks an applied edit nobody has looked at yet.
const toCheckSQL = `status = 'applied' AND patrolled_at IS NULL AND reverted_at IS NULL`

// CatalogueEditsToCheck is "Recent edits to check": the applied edits
// no reviewer has seen yet, newest first.
func (st *Store) CatalogueEditsToCheck(ctx context.Context) ([]CatalogueEdit, error) {
	return st.catalogueEdits(ctx, `e.status = 'applied' AND e.patrolled_at IS NULL AND e.reverted_at IS NULL
		ORDER BY e.created_at DESC, e.id DESC`)
}

// CatalogueAppliedSince is how many of personID's catalogue edits were
// applied since since (the daily limit).
func (st *Store) CatalogueAppliedSince(ctx context.Context, personID int64, since time.Time) (int, error) {
	var n int
	err := st.db.QueryRow(ctx, `
		SELECT count(*) FROM catalogue_edits WHERE author_id = $1 AND status = 'applied' AND created_at >= $2`,
		personID, since).Scan(&n)
	return n, err
}

// PersonHoldsSong reports whether songID is in one of personID's places:
// their own songbooks or set lists, their student songbook, a songbook or
// set list of a band they're a current member of, a songbook of a
// community they belong to (its repertoire, a course, a group's). Who
// holds a song plays it: their fix of it is a player's.
func (st *Store) PersonHoldsSong(ctx context.Context, personID, songID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		  WHERE se.variant_id = $2 AND (
		    sb.owner_person_id = $1
		    OR (sb.kind = 'student' AND sb.student_person_id = $1)
		    OR EXISTS (SELECT 1 FROM band_members bm WHERE bm.band_id = sb.owner_band_id AND bm.person_id = $1 AND upper_inf(bm.validity))
		    OR EXISTS (SELECT 1 FROM community_members cm WHERE cm.community_id = sb.owner_community_id AND cm.person_id = $1 AND upper_inf(cm.validity))))
		OR EXISTS (
		  SELECT 1 FROM setlist_items i JOIN setlists sl ON sl.id = i.setlist_id
		  WHERE i.variant_id = $2 AND (
		    sl.owner_person_id = $1
		    OR EXISTS (SELECT 1 FROM band_members bm WHERE bm.band_id = sl.owner_band_id AND bm.person_id = $1 AND upper_inf(bm.validity))))`,
		personID, songID).Scan(&ok)
	return ok, err
}

// PatrolCatalogueEdit marks applied edit id seen by reviewerID ("Looks
// good"): the staff or a trusted editor, not its author (but the staff).
func (st *Store) PatrolCatalogueEdit(ctx context.Context, id, reviewerID int64) error {
	e, err := st.CatalogueEditByID(ctx, id)
	if err != nil {
		return err
	}
	if err := st.mayReview(ctx, reviewerID, e); err != nil {
		return err
	}
	tag, err := st.db.Exec(ctx, `
		UPDATE catalogue_edits SET patrolled_at = now(), patrolled_by = $2
		WHERE id = $1 AND `+toCheckSQL, id, reviewerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrEditDecided
	}
	st.queueDrained(ctx)
	return nil
}

// RevertCatalogueEdit undoes applied (or approved) edit id, for
// reviewerID (the staff or a trusted editor, not its author but the
// staff), with a note its author is told: the song goes back to what it
// was before the edit where nothing changed since; when later edits came,
// only this one's changes are taken out (songdiff.Merge), ErrConflict
// when a later edit changed the same part (History reverts it by hand
// then). A revision is kept, by the reviewer; the edit is marked
// reverted (and seen).
func (st *Store) RevertCatalogueEdit(ctx context.Context, id, reviewerID int64, note string) error {
	e, err := st.CatalogueEditByID(ctx, id)
	if err != nil {
		return err
	}
	if err := st.mayReview(ctx, reviewerID, e); err != nil {
		return err
	}
	if (e.Status != EditApplied && e.Status != EditApproved) || e.RevertedAt != nil || e.RevisionID == nil {
		return ErrEditDecided
	}
	note = strings.TrimSpace(note)
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var cur VariantSnapshot
	var version int
	if err := tx.QueryRow(ctx, `SELECT title, artist, reference_url, body, version FROM songs WHERE id = $1 FOR UPDATE`, e.SongID).
		Scan(&cur.Title, &cur.Artist, &cur.ReferenceURL, &cur.Body, &version); err != nil {
		return err
	}
	before, after, err := revisionAround(ctx, tx, e.SongID, *e.RevisionID, cur)
	if err != nil {
		return err
	}
	target := before
	if cur.Body != after.Body || cur.Title != after.Title || cur.Artist != after.Artist || cur.ReferenceURL != after.ReferenceURL {
		// Later edits: take this one's changes out of the song as it is.
		for _, p := range songdiff.Plan(after.Body, cur.Body, before.Body) {
			if p.Conflict {
				return ErrConflict
			}
		}
		target = cur
		target.Body = songdiff.Merge(after.Body, cur.Body, before.Body, func(string) bool { return true })
		if cur.Title == after.Title {
			target.Title = before.Title
		}
		if cur.Artist == after.Artist {
			target.Artist = before.Artist
		}
		if cur.ReferenceURL == after.ReferenceURL {
			target.ReferenceURL = before.ReferenceURL
		}
	}
	lang, _ := st.PersonLocale(ctx, reviewerID)
	if lang == "" {
		lang = "en"
	}
	summary := i18n.T(lang, "Reverted %s’s edit", orElse(e.AuthorName, "?"))
	if note != "" {
		summary += ": " + note
	}
	if _, err := saveSongTx(ctx, tx, SongSave{
		SongID: e.SongID, Title: target.Title, Artist: target.Artist, ReferenceURL: target.ReferenceURL, Body: target.Body,
		ExpectVersion: strconv.Itoa(version), ActorID: reviewerID, Summary: summary,
	}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE catalogue_edits SET reverted_at = now(), reverted_by = $2,
		       patrolled_at = coalesce(patrolled_at, now()), patrolled_by = coalesce(patrolled_by, $2),
		       note = CASE WHEN $3 = '' THEN note ELSE $3 END
		WHERE id = $1`, id, reviewerID, note); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	e.Note = note
	st.tellAuthorReverted(ctx, e)
	st.queueDrained(ctx)
	return nil
}

// revisionAround is the song before revision revID (its snapshot) and
// right after it (the next revision's snapshot, or the song now, cur,
// when it's the latest).
func revisionAround(ctx context.Context, tx pgx.Tx, songID, revID int64, cur VariantSnapshot) (VariantSnapshot, VariantSnapshot, error) {
	var before, after VariantSnapshot
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT snapshot FROM revisions WHERE id = $1`, revID).Scan(&raw); err != nil {
		return before, after, err
	}
	if err := json.Unmarshal(raw, &before); err != nil {
		return before, after, err
	}
	err := tx.QueryRow(ctx, `
		SELECT n.snapshot FROM revisions r JOIN revisions n ON n.entity_type = r.entity_type AND n.entity_id = r.entity_id
		  AND (n.created_at, n.id) > (r.created_at, r.id)
		WHERE r.id = $1 ORDER BY n.created_at, n.id LIMIT 1`, revID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return before, cur, nil
	}
	if err != nil {
		return before, after, err
	}
	err = json.Unmarshal(raw, &after)
	return before, after, err
}

// tellAuthorReverted tells an edit's author it was reverted, and why (the
// reviewer's note).
func (st *Store) tellAuthorReverted(ctx context.Context, e *CatalogueEdit) {
	if e.AuthorID == 0 {
		return
	}
	lang, _ := st.PersonLocale(ctx, e.AuthorID)
	if lang == "" {
		lang = "en"
	}
	body := i18n.T(lang, "Your edit of %s was reverted.", e.SongTitle)
	if e.Note != "" {
		body += " " + i18n.T(lang, "The reviewer’s note: %s", e.Note)
	}
	if _, err := st.CreateNotification(ctx, e.AuthorID, NotificationCatalogueEditDecided, body, CatalogueEditLink(e.ID)); err != nil {
		log.Printf("catalogue edit %d: telling its author: %v", e.ID, err)
	}
}

// markEditsReverted: revision revID of songID reverted in History (the
// song back to before it): the catalogue edits it and the ones after it
// made are reverted.
func markEditsReverted(ctx context.Context, tx pgx.Tx, revID, actorID int64) error {
	_, err := tx.Exec(ctx, `
		UPDATE catalogue_edits e SET reverted_at = now(), reverted_by = $2,
		       patrolled_at = coalesce(e.patrolled_at, now()), patrolled_by = coalesce(e.patrolled_by, $2)
		FROM revisions r, revisions x
		WHERE r.id = $1 AND x.entity_type = r.entity_type AND x.entity_id = r.entity_id
		  AND (x.created_at, x.id) >= (r.created_at, r.id) AND e.revision_id = x.id AND e.reverted_at IS NULL`,
		revID, actorID)
	return err
}

// MayRevertCatalogue reports whether personID may revert revisionID of
// a catalogue song: its staff and trusted editors any; its author their
// own edit while it is the song's latest.
func (st *Store) MayRevertCatalogue(ctx context.Context, personID, revisionID int64) (bool, error) {
	s, err := st.CatalogueStandingOf(ctx, personID)
	if err != nil {
		return false, err
	}
	if s.Banned {
		return false, nil
	}
	if s.Staff || s.Trusted {
		return true, nil
	}
	var own, latest bool
	err = st.db.QueryRow(ctx, `
		SELECT r.actor_person_id IS NOT DISTINCT FROM $2,
		       NOT EXISTS (SELECT 1 FROM revisions l WHERE l.entity_type = r.entity_type AND l.entity_id = r.entity_id
		                   AND (l.created_at, l.id) > (r.created_at, r.id))
		FROM revisions r WHERE r.id = $1`, revisionID, personID).Scan(&own, &latest)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("no such revision %d: %w", revisionID, ErrNotFound)
	}
	return own && latest, err
}

// CreateRecordingVersion is a catalogue song for another recording of
// songID's (docs/ownership-model.md, "The catalogue: open edits"): a new
// song of The Big Songbook's, public, of the same work, made from it
// (forked_from_id), with the chart as edited (title, artist — the
// recording's performer —, reference, body) by personID, and its
// reference label (the album, "Live at …", "Cover"…: version_name).
// songID stays as it is. Its first revision is songID's chart, with
// summary.
func (st *Store) CreateRecordingVersion(ctx context.Context, songID, personID int64, title, artist, referenceURL, label, body, summary string) (int64, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var workID int64
	var origTitle, origArtist, origRef, origBody string
	var catalogue bool
	if err := tx.QueryRow(ctx, `
		SELECT work_id, title, artist, reference_url, body, `+catalogueSQL("s")+`
		FROM songs s WHERE id = $1`, songID).Scan(&workID, &origTitle, &origArtist, &origRef, &origBody, &catalogue); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("no such song %d: %w", songID, ErrNotFound)
		}
		return 0, err
	}
	if !catalogue {
		return 0, fmt.Errorf("song %d isn't the catalogue's: %w", songID, ErrForbidden)
	}
	var creator *int64
	if personID != 0 {
		creator = &personID
	}
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO songs (title, artist, body, reference_url, version_name, work_id, forked_from_id, owner_community_id, public, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, big_songbook_id(), true, $8) RETURNING id`,
		title, artist, body, referenceURL, strings.TrimSpace(label), workID, songID, creator).Scan(&id); err != nil {
		return 0, err
	}
	// Where it came from, in its history: the original's chart, then
	// this edit of it.
	if _, err := recordRevision(ctx, tx, "variant", strconv.FormatInt(id, 10), creator, VariantSnapshot{
		Title: origTitle, Artist: origArtist, ReferenceURL: origRef, Body: origBody,
	}, summary); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// CatalogueReport is a "Report" on a catalogue song.
type CatalogueReport struct {
	ID         int64
	SongID     int64
	SongTitle  string
	SongArtist string
	PersonID   int64
	PersonName string
	Reason     string
	HandledBy  *int64
	HandledAt  *time.Time
	CreatedAt  time.Time
}

// ReportCatalogueSong files personID's report on songID (a catalogue
// song: ErrForbidden otherwise) and tells the reviewers.
func (st *Store) ReportCatalogueSong(ctx context.Context, songID, personID int64, reason string) (int64, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return 0, errors.New("a report needs a reason")
	}
	var catalogue bool
	if err := st.db.QueryRow(ctx, `SELECT `+catalogueSQL("s")+` FROM songs s WHERE id = $1`, songID).Scan(&catalogue); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("no such song %d: %w", songID, ErrNotFound)
		}
		return 0, err
	}
	if !catalogue {
		return 0, fmt.Errorf("song %d isn't the catalogue's: %w", songID, ErrForbidden)
	}
	var id int64
	if err := st.db.QueryRow(ctx, `
		INSERT INTO catalogue_reports (song_id, person_id, reason) VALUES ($1, $2, $3) RETURNING id`,
		songID, personID, reason).Scan(&id); err != nil {
		return 0, err
	}
	st.tellCatalogueReviewers(ctx, personID)
	return id, nil
}

// OpenCatalogueReports is the reports not handled yet, oldest first.
func (st *Store) OpenCatalogueReports(ctx context.Context) ([]CatalogueReport, error) {
	rows, err := st.db.Query(ctx, `
		SELECT r.id, r.song_id, s.title, s.artist, coalesce(r.person_id, 0), coalesce(p.display_name, ''), r.reason,
		       r.handled_by, r.handled_at, r.created_at
		FROM catalogue_reports r JOIN songs s ON s.id = r.song_id LEFT JOIN people p ON p.id = r.person_id
		WHERE r.handled_at IS NULL ORDER BY r.created_at, r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CatalogueReport
	for rows.Next() {
		var c CatalogueReport
		if err := rows.Scan(&c.ID, &c.SongID, &c.SongTitle, &c.SongArtist, &c.PersonID, &c.PersonName, &c.Reason,
			&c.HandledBy, &c.HandledAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// HandleCatalogueReport marks report id handled by personID, a reviewer.
func (st *Store) HandleCatalogueReport(ctx context.Context, id, personID int64) error {
	if err := st.mayReview(ctx, personID, nil); err != nil {
		return err
	}
	tag, err := st.db.Exec(ctx, `
		UPDATE catalogue_reports SET handled_by = $2, handled_at = now() WHERE id = $1 AND handled_at IS NULL`, id, personID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no open report %d: %w", id, ErrNotFound)
	}
	st.queueDrained(ctx)
	return nil
}

// SetCatalogueBanned keeps personID from editing the catalogue (banned)
// or lets them again; staffID must be its staff. Their edits waiting for
// review are rejected with it.
func (st *Store) SetCatalogueBanned(ctx context.Context, staffID, personID int64, banned bool) error {
	s, err := st.CatalogueStandingOf(ctx, staffID)
	if err != nil {
		return err
	}
	if !s.Staff {
		return fmt.Errorf("only the catalogue's staff keep someone from editing it: %w", ErrForbidden)
	}
	if banned {
		if _, err := st.db.Exec(ctx, `UPDATE people SET catalogue_banned_at = coalesce(catalogue_banned_at, now()) WHERE id = $1 AND NOT is_admin`, personID); err != nil {
			return err
		}
		_, err = st.db.Exec(ctx, `
			UPDATE catalogue_edits SET status = 'rejected', decided_by = $2, decided_at = now()
			WHERE author_id = $1 AND status = 'pending'`, personID, staffID)
		st.queueDrained(ctx)
		return err
	}
	_, err = st.db.Exec(ctx, `UPDATE people SET catalogue_banned_at = NULL WHERE id = $1`, personID)
	return err
}

// BannedPerson is someone kept from editing the catalogue.
type BannedPerson struct {
	ID    int64
	Name  string
	Since time.Time
}

// CatalogueBannedPeople is who is kept from editing the catalogue.
func (st *Store) CatalogueBannedPeople(ctx context.Context) ([]BannedPerson, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, display_name, catalogue_banned_at FROM people WHERE catalogue_banned_at IS NOT NULL ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BannedPerson
	for rows.Next() {
		var b BannedPerson
		if err := rows.Scan(&b.ID, &b.Name, &b.Since); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SongChart is a song's chart as saved: its title, artist, reference,
// body and version.
type SongChart struct {
	Title, Artist, ReferenceURL, Body string
	Version                           int
}

// SongChartOf is songID's chart as saved.
func (st *Store) SongChartOf(ctx context.Context, songID int64) (*SongChart, error) {
	var c SongChart
	err := st.db.QueryRow(ctx, `SELECT title, artist, reference_url, body, version FROM songs WHERE id = $1`, songID).
		Scan(&c.Title, &c.Artist, &c.ReferenceURL, &c.Body, &c.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such song %d: %w", songID, ErrNotFound)
	}
	return &c, err
}

// SetReferenceLabel sets catalogue song songID's reference label (the
// album, "Live at …, 1994", "Original", "Cover"…: shown after its artist;
// version_name, which holds an arrangement's name on other songs), as an
// edit by personID: a revision kept (its snapshot carries the label
// before), the song's version moves on.
func (st *Store) SetReferenceLabel(ctx context.Context, songID, personID int64, label, summary string) error {
	label = strings.TrimSpace(label)
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var title, artist, ref, body, cur string
	var catalogue bool
	err = tx.QueryRow(ctx, `
		SELECT title, artist, reference_url, body, version_name, `+catalogueSQL("s")+`
		FROM songs s WHERE id = $1 FOR UPDATE`, songID).Scan(&title, &artist, &ref, &body, &cur, &catalogue)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no such song %d: %w", songID, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if !catalogue {
		return fmt.Errorf("song %d isn't the catalogue's: %w", songID, ErrForbidden)
	}
	if cur == label {
		return tx.Commit(ctx)
	}
	var actor *int64
	if personID != 0 {
		actor = &personID
	}
	if _, err := recordRevision(ctx, tx, "variant", strconv.FormatInt(songID, 10), actor, VariantSnapshot{
		Title: title, Artist: artist, ReferenceURL: ref, Body: body, VersionName: &cur,
	}, summary); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE songs SET version_name = $2, version = version + 1 WHERE id = $1`, songID, label); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
