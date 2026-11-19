package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Practice (migration 0084, docs/coaching.md): what a coach gives a
// student or a group to work on between lessons, and what each student
// practised, played in the app or told.

// PracticeAssignment is something to practise: a song of the community,
// or something written, for a student or a group.
type PracticeAssignment struct {
	ID          int64
	CommunityID int64
	PersonID    *int64
	GroupID     *int64
	GroupName   string
	VariantID   *int64
	SongTitle   string
	SongbookID  int64 // where the student opens the song: theirs, or the group's
	Title       string
	Notes       string
	PerWeek     int
	DueOn       *time.Time
	CreatedAt   time.Time
	DoneAt      *time.Time
	// For one student: the days practised this week (Monday on), and the
	// last time.
	DaysThisWeek  int
	LastPracticed *time.Time
}

// Label is what the assignment is called: its title, else its song's.
func (a PracticeAssignment) Label() string {
	if a.Title != "" {
		return a.Title
	}
	return a.SongTitle
}

// Overdue is an assignment past its date, not done.
func (a PracticeAssignment) Overdue(now time.Time) bool {
	if a.DueOn == nil || a.DoneAt != nil {
		return false
	}
	y, m, d := now.Date()
	return a.DueOn.Before(time.Date(y, m, d, 0, 0, 0, 0, a.DueOn.Location()))
}

// NewPracticeAssignment is what the coach fills in.
type NewPracticeAssignment struct {
	CommunityID int64
	PersonID    int64 // a student's, or
	GroupID     int64 // a group's
	VariantID   int64 // a song of the community, or 0 with a Title
	Title       string
	Notes       string
	PerWeek     int
	DueOn       *time.Time
	By          int64
}

// ErrBadAssignment is an assignment with nothing to practise, or no one.
var ErrBadAssignment = errors.New("an assignment needs a song or a title, and a student or a group")

// CreatePracticeAssignment adds an assignment.
func (st *Store) CreatePracticeAssignment(ctx context.Context, a NewPracticeAssignment) (int64, error) {
	if (a.PersonID == 0) == (a.GroupID == 0) || (a.VariantID == 0 && a.Title == "") {
		return 0, ErrBadAssignment
	}
	a.PerWeek = max(0, min(7, a.PerWeek))
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO practice_assignments (community_id, person_id, group_id, variant_id, title, notes, per_week, due_on, created_by)
		VALUES ($1, nullif($2, 0::bigint), nullif($3, 0::bigint), nullif($4, 0::bigint), $5, $6, $7, $8, nullif($9, 0::bigint))
		RETURNING id`, a.CommunityID, a.PersonID, a.GroupID, a.VariantID, a.Title, a.Notes, a.PerWeek, a.DueOn, a.By).Scan(&id)
	return id, err
}

// SetPracticeAssignmentDone marks an assignment done (or not) in its
// community.
func (st *Store) SetPracticeAssignmentDone(ctx context.Context, communityID, id int64, done bool) error {
	tag, err := st.db.Exec(ctx, `
		UPDATE practice_assignments SET done_at = CASE WHEN $3 THEN coalesce(done_at, now()) END
		WHERE id = $1 AND community_id = $2`, id, communityID, done)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// DeletePracticeAssignment removes an assignment of the community; its
// practice stays logged.
func (st *Store) DeletePracticeAssignment(ctx context.Context, communityID, id int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM practice_assignments WHERE id = $1 AND community_id = $2`, id, communityID)
	return err
}

// GetPracticeAssignment is one assignment of the community.
func (st *Store) GetPracticeAssignment(ctx context.Context, communityID, id int64) (*PracticeAssignment, error) {
	list, err := st.listAssignments(ctx, `a.id = $1 AND a.community_id = $2`, 0, true, id, communityID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

// ListStudentAssignments is what a student has to practise in a
// community: their own and their current groups', open ones first, with
// their days this week; done ones too with withDone.
func (st *Store) ListStudentAssignments(ctx context.Context, communityID, personID int64, withDone bool) ([]PracticeAssignment, error) {
	return st.listAssignments(ctx, `a.community_id = $1 AND (a.person_id = $2
		OR a.group_id IN (SELECT group_id FROM coaching_group_members WHERE person_id = $2))`, personID, withDone, communityID, personID)
}

// ListGroupAssignments is a group's assignments.
func (st *Store) ListGroupAssignments(ctx context.Context, groupID int64, withDone bool) ([]PracticeAssignment, error) {
	return st.listAssignments(ctx, `a.group_id = $1`, 0, withDone, groupID)
}

// listAssignments lists the assignments matching where; with a student
// (forPerson), their days practised this week and last practice.
func (st *Store) listAssignments(ctx context.Context, where string, forPerson int64, withDone bool, args ...any) ([]PracticeAssignment, error) {
	if !withDone {
		where += ` AND a.done_at IS NULL`
	}
	args = append(args, forPerson)
	p := len(args)
	rows, err := st.db.Query(ctx, `
		SELECT a.id, a.community_id, a.person_id, a.group_id, coalesce(g.name, ''), a.variant_id, coalesce(s.title, ''),
		       coalesce(CASE WHEN a.group_id IS NOT NULL THEN g.songbook_id
		                     ELSE (SELECT sb.id FROM songbooks sb WHERE sb.owner_community_id = a.community_id
		                           AND sb.kind = 'student' AND sb.student_person_id = a.person_id LIMIT 1) END, 0),
		       a.title, a.notes, a.per_week, a.due_on, a.created_at, a.done_at,
		       (SELECT count(DISTINCT l.practiced_on) FROM practice_logs l
		        WHERE l.assignment_id = a.id AND l.person_id = $`+strconv.Itoa(p)+` AND l.practiced_on >= date_trunc('week', current_date)),
		       (SELECT max(l.practiced_on)::timestamptz FROM practice_logs l WHERE l.assignment_id = a.id AND l.person_id = $`+strconv.Itoa(p)+`)
		FROM practice_assignments a
		LEFT JOIN coaching_groups g ON g.id = a.group_id
		LEFT JOIN songs s ON s.id = a.variant_id
		WHERE `+where+`
		ORDER BY (a.done_at IS NOT NULL), a.due_on NULLS LAST, a.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PracticeAssignment
	for rows.Next() {
		var a PracticeAssignment
		if err := rows.Scan(&a.ID, &a.CommunityID, &a.PersonID, &a.GroupID, &a.GroupName, &a.VariantID, &a.SongTitle, &a.SongbookID,
			&a.Title, &a.Notes, &a.PerWeek, &a.DueOn, &a.CreatedAt, &a.DoneAt, &a.DaysThisWeek, &a.LastPracticed); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PracticeLog is a day's practice.
type PracticeLog struct {
	ID           int64
	AssignmentID *int64
	VariantID    *int64
	Label        string // the assignment's or the song's
	PracticedOn  time.Time
	Seconds      int
	Source       string // "player" or "manual"
	Note         string
}

// Minutes is the practice rounded to the minute, at least one.
func (l PracticeLog) Minutes() int { return max(1, (l.Seconds+30)/60) }

// LogPractice is what a student says they practised: so many minutes,
// on an assignment of theirs (or none), with a note.
func (st *Store) LogPractice(ctx context.Context, communityID, personID int64, assignmentID int64, day time.Time, seconds int, note string) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO practice_logs (person_id, community_id, assignment_id, variant_id, practiced_on, seconds, source, note)
		VALUES ($1, $2, nullif($3, 0::bigint), (SELECT variant_id FROM practice_assignments WHERE id = $3), $4, $5, 'manual', $6)
		RETURNING id`, personID, communityID, assignmentID, day, max(0, seconds), note).Scan(&id)
	return id, err
}

// LogPlayed adds time a student played a song of a coaching community's
// songbook of theirs (their own, or a group's they're in): one row a
// song a day, on the open assignment of that song if there's one.
// Played elsewhere, or by someone else (the coach), nothing is logged.
func (st *Store) LogPlayed(ctx context.Context, personID, songbookID, variantID int64, seconds int) (bool, error) {
	if seconds <= 0 {
		return false, nil
	}
	seconds = min(seconds, 3600)
	var communityID int64
	err := st.db.QueryRow(ctx, `
		SELECT sb.owner_community_id FROM songbooks sb
		JOIN communities c ON c.id = sb.owner_community_id AND c.kind = 'coaching'
		JOIN songbook_entries e ON e.songbook_id = sb.id AND e.variant_id = $3
		WHERE sb.id = $2 AND (
		  (sb.kind = 'student' AND sb.student_person_id = $1)
		  OR (sb.kind = 'group' AND EXISTS (SELECT 1 FROM coaching_groups g JOIN coaching_group_members m ON m.group_id = g.id
		                                    WHERE g.songbook_id = sb.id AND m.person_id = $1)))`,
		personID, songbookID, variantID).Scan(&communityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = st.db.Exec(ctx, `
		INSERT INTO practice_logs (person_id, community_id, assignment_id, variant_id, practiced_on, seconds, source)
		VALUES ($1, $2, (SELECT a.id FROM practice_assignments a WHERE a.community_id = $2 AND a.variant_id = $3 AND a.done_at IS NULL
		                 AND (a.person_id = $1 OR a.group_id IN (SELECT group_id FROM coaching_group_members WHERE person_id = $1))
		                 ORDER BY a.created_at DESC LIMIT 1),
		        $3, current_date, $4, 'player')
		ON CONFLICT (person_id, community_id, variant_id, practiced_on) WHERE source = 'player'
		DO UPDATE SET seconds = least(practice_logs.seconds + excluded.seconds, 6 * 3600),
		              assignment_id = coalesce(practice_logs.assignment_id, excluded.assignment_id)`,
		personID, communityID, variantID, seconds)
	return err == nil, err
}

// DeletePracticeLog removes a log the student told (not one the player
// counted).
func (st *Store) DeletePracticeLog(ctx context.Context, communityID, personID, id int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM practice_logs WHERE id = $1 AND community_id = $2 AND person_id = $3 AND source = 'manual'`, id, communityID, personID)
	return err
}

// ListPracticeLogs is a student's practice in a community since a day,
// latest first.
func (st *Store) ListPracticeLogs(ctx context.Context, communityID, personID int64, since time.Time) ([]PracticeLog, error) {
	rows, err := st.db.Query(ctx, `
		SELECT l.id, l.assignment_id, l.variant_id,
		       coalesce(nullif(a.title, ''), s.title, ''), l.practiced_on::timestamptz, l.seconds, l.source, l.note
		FROM practice_logs l
		LEFT JOIN practice_assignments a ON a.id = l.assignment_id
		LEFT JOIN songs s ON s.id = l.variant_id
		WHERE l.community_id = $1 AND l.person_id = $2 AND l.practiced_on >= $3
		ORDER BY l.practiced_on DESC, l.created_at DESC`, communityID, personID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PracticeLog
	for rows.Next() {
		var l PracticeLog
		if err := rows.Scan(&l.ID, &l.AssignmentID, &l.VariantID, &l.Label, &l.PracticedOn, &l.Seconds, &l.Source, &l.Note); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// PracticeSummary is a student's practice over the last seven days.
type PracticeSummary struct {
	Seconds int
	Days    int
	Last    *time.Time
	Open    int // assignments to practise
}

// Minutes is the week's practice in minutes.
func (p PracticeSummary) Minutes() int { return (p.Seconds + 30) / 60 }

// CommunityPracticeSummaries is every student's practice summary in a
// community, by person.
func (st *Store) CommunityPracticeSummaries(ctx context.Context, communityID int64) (map[int64]PracticeSummary, error) {
	rows, err := st.db.Query(ctx, `
		SELECT m.person_id,
		       coalesce((SELECT sum(seconds) FROM practice_logs l WHERE l.community_id = $1 AND l.person_id = m.person_id AND l.practiced_on > current_date - 7), 0),
		       (SELECT count(DISTINCT practiced_on) FROM practice_logs l WHERE l.community_id = $1 AND l.person_id = m.person_id AND l.practiced_on > current_date - 7),
		       (SELECT max(practiced_on)::timestamptz FROM practice_logs l WHERE l.community_id = $1 AND l.person_id = m.person_id),
		       (SELECT count(*) FROM practice_assignments a WHERE a.community_id = $1 AND a.done_at IS NULL
		        AND (a.person_id = m.person_id OR a.group_id IN (SELECT group_id FROM coaching_group_members WHERE person_id = m.person_id)))
		FROM community_members m WHERE m.community_id = $1 AND upper_inf(m.validity)`, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]PracticeSummary{}
	for rows.Next() {
		var id int64
		var p PracticeSummary
		if err := rows.Scan(&id, &p.Seconds, &p.Days, &p.Last, &p.Open); err != nil {
			return nil, err
		}
		out[id] = p
	}
	return out, rows.Err()
}

// CommunityHasSong is whether a song is in one of the community's
// songbooks.
func (st *Store) CommunityHasSong(ctx context.Context, communityID, variantID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM songbook_entries e JOIN songbooks sb ON sb.id = e.songbook_id
		               WHERE sb.owner_community_id = $1 AND e.variant_id = $2)`, communityID, variantID).Scan(&ok)
	return ok, err
}
