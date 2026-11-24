package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// A coaching community's songbooks seen from both sides (docs/coaching.md,
// "The songbooks"): a student asks for a song of the repertoire ("Ask for
// it", migration 0107), the coach adds it to their songbook in their key
// or lets it go; and what the coach's card of each student says (the last
// song worked on, the takes since the last lesson).

// Song request statuses.
const (
	SongRequestPending  = "pending"
	SongRequestAdded    = "added"
	SongRequestDeclined = "declined"
)

// Request kinds: a song of the repertoire asked for, or a song of the
// student's own proposed (migration 0128, coaching_proposals.go).
const (
	SongRequestAsk     = "ask"
	SongRequestPropose = "propose"
)

// SongRequest is a student's request for a song of the repertoire, or
// their proposal of a song of their own (Kind).
type SongRequest struct {
	ID          int64
	CommunityID int64
	PersonID    int64
	StudentName string
	VariantID   int64
	Title       string
	Artist      string
	Status      string
	CreatedAt   time.Time
	Kind        string
	// A proposal's: the student's note, the key they'd like (semitones
	// from the song's own key, nil: none said); the coach's answer, and
	// the song that entered the repertoire (0: none yet).
	Note            string
	WishedSemitones *int
	Reply           string
	ResultVariantID int64
	DecidedAt       *time.Time
}

// IsProposal reports whether the request is the student's own song
// proposed, not a song of the repertoire asked for.
func (q SongRequest) IsProposal() bool { return q.Kind == SongRequestPropose }

// RepertoireSongbook is a community's repertoire: its default songbook, 0
// when it has none.
func (st *Store) RepertoireSongbook(ctx context.Context, communityID int64) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `SELECT id FROM songbooks WHERE owner_community_id = $1 AND is_default ORDER BY id LIMIT 1`, communityID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// RepertoireHas reports whether a song is in the community's repertoire,
// the only songs a student may ask for.
func (st *Store) RepertoireHas(ctx context.Context, communityID, variantID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM songbook_entries e JOIN songbooks sb ON sb.id = e.songbook_id
		               WHERE sb.owner_community_id = $1 AND sb.is_default AND e.variant_id = $2)`, communityID, variantID).Scan(&ok)
	return ok, err
}

// RequestSong files a student's request for a song: created is false when
// the same request is already waiting (nothing new to tell the coach). A
// request the coach let go, or one added and since taken out, waits again.
func (st *Store) RequestSong(ctx context.Context, communityID, personID, variantID int64) (id int64, created bool, err error) {
	err = st.db.QueryRow(ctx, `
		INSERT INTO coaching_song_requests (community_id, person_id, variant_id) VALUES ($1, $2, $3)
		ON CONFLICT (community_id, person_id, variant_id) DO UPDATE SET
			status = 'pending', created_at = now(), decided_at = NULL, decided_by_person_id = NULL,
			kind = 'ask', note = '', wished_semitones = NULL, reply = '', result_variant_id = NULL
			WHERE coaching_song_requests.status <> 'pending'
		RETURNING id`, communityID, personID, variantID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Already waiting: the update's WHERE left it alone.
		err = st.db.QueryRow(ctx, `SELECT id FROM coaching_song_requests WHERE community_id = $1 AND person_id = $2 AND variant_id = $3`,
			communityID, personID, variantID).Scan(&id)
		return id, false, err
	}
	return id, err == nil, err
}

const songRequestColumns = `r.id, r.community_id, r.person_id, p.display_name, r.variant_id, s.title, s.artist, r.status, r.created_at,
	r.kind, r.note, r.wished_semitones, r.reply, coalesce(r.result_variant_id, 0), r.decided_at
	FROM coaching_song_requests r JOIN people p ON p.id = r.person_id JOIN songs s ON s.id = r.variant_id`

func scanSongRequest(row pgx.Row) (SongRequest, error) {
	var q SongRequest
	err := row.Scan(&q.ID, &q.CommunityID, &q.PersonID, &q.StudentName, &q.VariantID, &q.Title, &q.Artist, &q.Status, &q.CreatedAt,
		&q.Kind, &q.Note, &q.WishedSemitones, &q.Reply, &q.ResultVariantID, &q.DecidedAt)
	return q, err
}

// GetSongRequest is one request, ErrNotFound when there's none.
func (st *Store) GetSongRequest(ctx context.Context, id int64) (*SongRequest, error) {
	q, err := scanSongRequest(st.db.QueryRow(ctx, `SELECT `+songRequestColumns+` WHERE r.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// PendingSongRequests are the requests waiting on a community's coach, the
// oldest first.
func (st *Store) PendingSongRequests(ctx context.Context, communityID int64) ([]SongRequest, error) {
	rows, err := st.db.Query(ctx, `SELECT `+songRequestColumns+`
		WHERE r.community_id = $1 AND r.status = 'pending' ORDER BY r.created_at, r.id`, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SongRequest
	for rows.Next() {
		q, err := scanSongRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// StudentSongRequests are a student's requests in a community, by song:
// the status of each.
func (st *Store) StudentSongRequests(ctx context.Context, communityID, personID int64) (map[int64]string, error) {
	rows, err := st.db.Query(ctx, `SELECT variant_id, status FROM coaching_song_requests WHERE community_id = $1 AND person_id = $2`, communityID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var v int64
		var s string
		if err := rows.Scan(&v, &s); err != nil {
			return nil, err
		}
		out[v] = s
	}
	return out, rows.Err()
}

// DecideSongRequest closes a waiting request (SongRequestAdded or
// SongRequestDeclined): ErrNotFound when it isn't waiting any more (the
// other coach was quicker).
func (st *Store) DecideSongRequest(ctx context.Context, id int64, status string, byPersonID int64) error {
	return st.AnswerSongRequest(ctx, id, status, byPersonID, "")
}

// AnswerSongRequest is DecideSongRequest with the coach's note to the
// student (a proposal declined: why, what instead).
func (st *Store) AnswerSongRequest(ctx context.Context, id int64, status string, byPersonID int64, reply string) error {
	if status != SongRequestAdded && status != SongRequestDeclined {
		return errors.New("a request is added or declined")
	}
	tag, err := st.db.Exec(ctx, `
		UPDATE coaching_song_requests SET status = $2, decided_at = now(), decided_by_person_id = $3, reply = $4
		WHERE id = $1 AND status = 'pending'`, id, status, nullID(byPersonID), reply)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkNotificationsReadByLink marks every unread notification of a kind
// pointing at link read, whoever's it is: a request one coach handled is
// handled for the others too.
func (st *Store) MarkNotificationsReadByLink(ctx context.Context, kind, link string) error {
	_, err := st.db.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE kind = $1 AND link = $2 AND read_at IS NULL`, kind, link)
	return err
}

// WorkedOn is the last song a student practised in a community.
type WorkedOn struct {
	VariantID int64
	Title     string
	On        time.Time
}

// LastSongsWorkedOn is each student's last song practised in a community
// (played in the app or told), by person.
func (st *Store) LastSongsWorkedOn(ctx context.Context, communityID int64) (map[int64]WorkedOn, error) {
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT ON (l.person_id) l.person_id, l.variant_id, s.title, l.practiced_on::timestamptz
		FROM practice_logs l JOIN songs s ON s.id = l.variant_id
		WHERE l.community_id = $1
		ORDER BY l.person_id, l.practiced_on DESC, l.created_at DESC`, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]WorkedOn{}
	for rows.Next() {
		var id int64
		var w WorkedOn
		if err := rows.Scan(&id, &w.VariantID, &w.Title, &w.On); err != nil {
			return nil, err
		}
		out[id] = w
	}
	return out, rows.Err()
}

// TakesSince counts a student's checked takes (songs and warm-ups) in a
// community since a time: what the coach has to listen to before the next
// lesson.
func (st *Store) TakesSince(ctx context.Context, communityID, personID int64, since time.Time) (int, error) {
	var n int
	err := st.db.QueryRow(ctx, `SELECT count(*) FROM practice_takes WHERE community_id = $1 AND person_id = $2 AND created_at > $3`,
		communityID, personID, since).Scan(&n)
	return n, err
}

// SongbookKeys is each song's key in a songbook, in semitones from its
// own, by song: only those moved (a student's, a group's).
func (st *Store) SongbookKeys(ctx context.Context, songbookID int64) (map[int64]int, error) {
	rows, err := st.db.Query(ctx, `SELECT variant_id, semitones FROM songbook_entries WHERE songbook_id = $1 AND semitones <> 0`, songbookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var v int64
		var n int
		if err := rows.Scan(&v, &n); err != nil {
			return nil, err
		}
		out[v] = n
	}
	return out, rows.Err()
}

// UnreadNotificationsAt is personID's unread notifications of a kind
// pointing at link: how many, and the latest's text.
func (st *Store) UnreadNotificationsAt(ctx context.Context, personID int64, kind, link string) (int, string, error) {
	var n int
	var body string
	err := st.db.QueryRow(ctx, `
		SELECT count(*), coalesce((array_agg(body ORDER BY created_at DESC, id DESC))[1], '')
		FROM notifications WHERE person_id = $1 AND kind = $2 AND link = $3 AND read_at IS NULL`, personID, kind, link).Scan(&n, &body)
	return n, body, err
}
