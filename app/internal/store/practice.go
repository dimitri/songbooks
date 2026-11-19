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
	ExerciseID  *int64 // or an exercise (warm-ups)
	Exercise    string
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
	// The coach's goal on a song (pitch-check.js): a level, the share of
	// notes in tune and on time to reach; when reached; for one student,
	// their best take at that level.
	Level        string
	PitchTarget  int
	RhythmTarget int
	ReachedAt    *time.Time
	BestPitch    *int
	BestRhythm   *int
}

// HasGoal is whether the coach set a target to reach.
func (a PracticeAssignment) HasGoal() bool { return a.PitchTarget > 0 || a.RhythmTarget > 0 }

// Label is what the assignment is called: its title, else its song's.
func (a PracticeAssignment) Label() string {
	if a.Title != "" {
		return a.Title
	}
	if a.Exercise != "" {
		return a.Exercise
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
	VariantID   int64 // a song of the community, or
	ExerciseID  int64 // an exercise of it, or 0 with a Title
	Title       string
	Notes       string
	PerWeek     int
	DueOn       *time.Time
	By          int64
	// A goal for a song: a level, and targets in % (0: none).
	Level        string
	PitchTarget  int
	RhythmTarget int
}

// ErrBadAssignment is an assignment with nothing to practise, or no one.
var ErrBadAssignment = errors.New("an assignment needs a song or a title, and a student or a group")

// CreatePracticeAssignment adds an assignment.
func (st *Store) CreatePracticeAssignment(ctx context.Context, a NewPracticeAssignment) (int64, error) {
	if (a.PersonID == 0) == (a.GroupID == 0) || (a.VariantID == 0 && a.ExerciseID == 0 && a.Title == "") || (a.VariantID != 0 && a.ExerciseID != 0) {
		return 0, ErrBadAssignment
	}
	a.PerWeek = max(0, min(7, a.PerWeek))
	a.PitchTarget, a.RhythmTarget = max(0, min(100, a.PitchTarget)), max(0, min(100, a.RhythmTarget))
	var level *string
	if (a.VariantID != 0 || a.ExerciseID != 0) && (a.PitchTarget > 0 || a.RhythmTarget > 0) {
		if !takeLevels[a.Level] {
			a.Level = "beginner"
		}
		level = &a.Level
	} else {
		a.PitchTarget, a.RhythmTarget = 0, 0
	}
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO practice_assignments (community_id, person_id, group_id, variant_id, title, notes, per_week, due_on, created_by, level, pitch_target, rhythm_target, exercise_id)
		VALUES ($1, nullif($2, 0::bigint), nullif($3, 0::bigint), nullif($4, 0::bigint), $5, $6, $7, $8, nullif($9, 0::bigint), $10, nullif($11, 0), nullif($12, 0), nullif($13, 0::bigint))
		RETURNING id`, a.CommunityID, a.PersonID, a.GroupID, a.VariantID, a.Title, a.Notes, a.PerWeek, a.DueOn, a.By, level, a.PitchTarget, a.RhythmTarget, a.ExerciseID).Scan(&id)
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
		SELECT a.id, a.community_id, a.person_id, a.group_id, coalesce(g.name, ''), a.variant_id, coalesce(s.title, ''), a.exercise_id, coalesce(x.name, ''),
		       coalesce(CASE WHEN a.group_id IS NOT NULL THEN g.songbook_id
		                     ELSE (SELECT sb.id FROM songbooks sb WHERE sb.owner_community_id = a.community_id
		                           AND sb.kind = 'student' AND sb.student_person_id = a.person_id LIMIT 1) END, 0),
		       a.title, a.notes, a.per_week, a.due_on, a.created_at, a.done_at,
		       (SELECT count(DISTINCT l.practiced_on) FROM practice_logs l
		        WHERE l.assignment_id = a.id AND l.person_id = $`+strconv.Itoa(p)+` AND l.practiced_on >= date_trunc('week', current_date)),
		       (SELECT max(l.practiced_on)::timestamptz FROM practice_logs l WHERE l.assignment_id = a.id AND l.person_id = $`+strconv.Itoa(p)+`),
		       coalesce(a.level, ''), coalesce(a.pitch_target, 0), coalesce(a.rhythm_target, 0), a.reached_at,
		       (SELECT max(t.pitch) FROM practice_takes t WHERE t.person_id = $`+strconv.Itoa(p)+` AND (t.variant_id = a.variant_id OR t.exercise_id = a.exercise_id)
		        AND t.level = coalesce(a.level, 'beginner') AND t.created_at >= a.created_at),
		       (SELECT max(t.rhythm) FROM practice_takes t WHERE t.person_id = $`+strconv.Itoa(p)+` AND (t.variant_id = a.variant_id OR t.exercise_id = a.exercise_id)
		        AND t.level = coalesce(a.level, 'beginner') AND t.created_at >= a.created_at)
		FROM practice_assignments a
		LEFT JOIN coaching_groups g ON g.id = a.group_id
		LEFT JOIN songs s ON s.id = a.variant_id
		LEFT JOIN exercises x ON x.id = a.exercise_id
		WHERE `+where+`
		ORDER BY (a.done_at IS NOT NULL), a.due_on NULLS LAST, a.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PracticeAssignment
	for rows.Next() {
		var a PracticeAssignment
		if err := rows.Scan(&a.ID, &a.CommunityID, &a.PersonID, &a.GroupID, &a.GroupName, &a.VariantID, &a.SongTitle, &a.ExerciseID, &a.Exercise, &a.SongbookID,
			&a.Title, &a.Notes, &a.PerWeek, &a.DueOn, &a.CreatedAt, &a.DoneAt, &a.DaysThisWeek, &a.LastPracticed,
			&a.Level, &a.PitchTarget, &a.RhythmTarget, &a.ReachedAt, &a.BestPitch, &a.BestRhythm); err != nil {
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
	PitchTakes   int  // takes checked that day (pitch-check.js)
	PitchScore   *int // the last one's notes in tune, %
	RhythmScore  *int // and on time, %
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
	communityID, err := st.studentSongbookCommunity(ctx, personID, songbookID, variantID)
	if err != nil || communityID == 0 {
		return false, err
	}
	_, err = st.db.Exec(ctx, `
		INSERT INTO practice_logs (person_id, community_id, assignment_id, variant_id, practiced_on, seconds, source)
		VALUES ($1, $2, `+openAssignmentOf+`, $3, current_date, $4, 'player')
		ON CONFLICT (person_id, community_id, variant_id, practiced_on) WHERE source = 'player'
		DO UPDATE SET seconds = least(practice_logs.seconds + excluded.seconds, 6 * 3600),
		              assignment_id = coalesce(practice_logs.assignment_id, excluded.assignment_id)`,
		personID, communityID, variantID, min(seconds, 3600))
	return err == nil, err
}

// Levels of strictness for a checked take (pitch-check.js): in tune
// within 50, 25 or 12 cents, on time within 200, 120 or 70 ms.
var takeLevels = map[string]bool{"beginner": true, "intermediate": true, "advanced": true}

// Take is a take checked by pitch-check.js.
type Take struct {
	Level         string
	Pitch, Rhythm int // % of the notes sung, in tune; on time
	Cents, Ms     int // tendencies: flat, early below 0
	Notes, Hits   int
	Seconds       int
}

// studentSongbookCommunity is the coaching community of a songbook of
// the person's (their own, or their group's) holding the song; 0 when
// it's none (someone else's, or not a coaching community's).
func (st *Store) studentSongbookCommunity(ctx context.Context, personID, songbookID, variantID int64) (int64, error) {
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
		return 0, nil
	}
	return communityID, err
}

// RecordTake keeps a checked take of a student's song (LogPlayed's rule):
// in the takes, on the day's practice of the song, and against the goal
// of its open assignment, reached when both scores are at the goal's
// level and targets.
func (st *Store) RecordTake(ctx context.Context, personID, songbookID, variantID int64, tk Take) (bool, error) {
	communityID, err := st.studentSongbookCommunity(ctx, personID, songbookID, variantID)
	if err != nil || communityID == 0 {
		return false, err
	}
	return true, st.recordTake(ctx, personID, communityID, subject{col: "variant_id", id: variantID, songbookID: songbookID}, tk)
}

// RecordExerciseTake keeps a checked take of an exercise, for a student of
// its community, as RecordTake does a song's.
func (st *Store) RecordExerciseTake(ctx context.Context, personID, exerciseID int64, tk Take) (bool, error) {
	communityID, err := st.exerciseStudentCommunity(ctx, personID, exerciseID)
	if err != nil || communityID == 0 {
		return false, err
	}
	return true, st.recordTake(ctx, personID, communityID, subject{col: "exercise_id", id: exerciseID}, tk)
}

// subject is what a take is of: a song (variant_id) or an exercise
// (exercise_id).
type subject struct {
	col        string
	id         int64
	songbookID int64
}

// exerciseStudentCommunity is an exercise's community when the person is
// one of its students (current, not staff); 0 otherwise.
func (st *Store) exerciseStudentCommunity(ctx context.Context, personID, exerciseID int64) (int64, error) {
	var communityID int64
	err := st.db.QueryRow(ctx, `
		SELECT x.community_id FROM exercises x
		JOIN community_members cm ON cm.community_id = x.community_id AND cm.person_id = $1 AND upper_inf(cm.validity)
		LEFT JOIN community_roles r ON r.id = cm.role_id
		WHERE x.id = $2 AND coalesce(r.name, '') <> 'staff'`, personID, exerciseID).Scan(&communityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return communityID, err
}

func (st *Store) recordTake(ctx context.Context, personID, communityID int64, sub subject, tk Take) error {
	if !takeLevels[tk.Level] {
		return errors.New("invalid level")
	}
	clamp := func(v, lo, hi int) int { return max(lo, min(hi, v)) }
	tk.Pitch, tk.Rhythm = clamp(tk.Pitch, 0, 100), clamp(tk.Rhythm, 0, 100)
	tk.Cents, tk.Ms = clamp(tk.Cents, -1200, 1200), clamp(tk.Ms, -2000, 2000)
	settings, err := st.GetCheckSettings(ctx, communityID)
	if err != nil {
		return err
	}
	margin := settings.Level(tk.Level)
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var songbook *int64
	if sub.songbookID != 0 {
		songbook = &sub.songbookID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_takes (person_id, community_id, `+sub.col+`, songbook_id, level, pitch, rhythm, cents, ms, notes, hits, seconds, cents_margin, ms_margin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		personID, communityID, sub.id, songbook, tk.Level, tk.Pitch, tk.Rhythm, tk.Cents, tk.Ms, tk.Notes, tk.Hits, clamp(tk.Seconds, 0, 3600), margin.Cents, margin.Ms); err != nil {
		return err
	}
	conflict := `(person_id, community_id, variant_id, practiced_on) WHERE source = 'player'`
	if sub.col == "exercise_id" {
		conflict = `(person_id, community_id, exercise_id, practiced_on) WHERE source = 'player' AND exercise_id IS NOT NULL`
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO practice_logs (person_id, community_id, assignment_id, `+sub.col+`, practiced_on, seconds, source, pitch_takes, pitch_score, rhythm_score)
		VALUES ($1, $2, `+openAssignment(sub.col)+`, $3, current_date, $6, 'player', 1, $4, $5)
		ON CONFLICT `+conflict+`
		DO UPDATE SET assignment_id = coalesce(practice_logs.assignment_id, excluded.assignment_id),
		              seconds = least(practice_logs.seconds + excluded.seconds, 6 * 3600),
		              pitch_takes = practice_logs.pitch_takes + 1,
		              pitch_score = excluded.pitch_score, rhythm_score = excluded.rhythm_score`,
		personID, communityID, sub.id, tk.Pitch, tk.Rhythm, map[bool]int{true: clamp(tk.Seconds, 0, 3600), false: 0}[sub.col == "exercise_id"]); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE practice_assignments a SET reached_at = now()
		WHERE a.id = `+openAssignment(sub.col)+` AND a.reached_at IS NULL
		  AND (a.pitch_target IS NOT NULL OR a.rhythm_target IS NOT NULL)
		  AND coalesce(a.level, 'beginner') = $6
		  AND $4 >= coalesce(a.pitch_target, 0) AND $5 >= coalesce(a.rhythm_target, 0)`,
		personID, communityID, sub.id, tk.Pitch, tk.Rhythm, tk.Level); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// openAssignment is the newest open assignment of a song or an exercise
// (col, $3) for a student ($1) in a community ($2), theirs or their
// group's.
func openAssignment(col string) string {
	return `(SELECT a.id FROM practice_assignments a WHERE a.community_id = $2 AND a.` + col + ` = $3 AND a.done_at IS NULL
		AND (a.person_id = $1 OR a.group_id IN (SELECT group_id FROM coaching_group_members WHERE person_id = $1))
		ORDER BY a.created_at DESC LIMIT 1)`
}

// openAssignmentOf is openAssignment of a song.
var openAssignmentOf = openAssignment("variant_id")

// TakeScore is a take's two scores, and when.
type TakeScore struct {
	Pitch  int       `json:"pitch"`
	Rhythm int       `json:"rhythm"`
	At     time.Time `json:"at"`
}

// SongProgress is a student's progress on a song: the coach's goal (from
// its open assignment), their best and last takes, their streak of days.
type SongProgress struct {
	Level        string      `json:"level,omitempty"` // the coach's, "" when none set
	PitchTarget  int         `json:"pitchTarget,omitempty"`
	RhythmTarget int         `json:"rhythmTarget,omitempty"`
	Reached      bool        `json:"reached"`
	Best         *TakeScore  `json:"best,omitempty"` // at the goal's level, else any
	Takes        []TakeScore `json:"takes"`          // the last ones, oldest first
	Streak       int         `json:"streak"`         // days in a row with practice, to today or yesterday
	// The coach's settings (levels, messages), the student's first name
	// and the coach's, for the messages.
	Settings CheckSettings `json:"settings"`
	Name     string        `json:"name"`
	Coach    string        `json:"coach"`
}

// SongProgressOf is a student's progress on a song of their songbook;
// nil when it isn't one (LogPlayed's rule).
func (st *Store) SongProgressOf(ctx context.Context, personID, songbookID, variantID int64) (*SongProgress, error) {
	communityID, err := st.studentSongbookCommunity(ctx, personID, songbookID, variantID)
	if err != nil || communityID == 0 {
		return nil, err
	}
	return st.progressOf(ctx, personID, communityID, subject{col: "variant_id", id: variantID})
}

// ExerciseProgressOf is a student's progress on an exercise of their
// community (nil for anyone else), its own words over the community's.
func (st *Store) ExerciseProgressOf(ctx context.Context, personID, exerciseID int64) (*SongProgress, error) {
	communityID, err := st.exerciseStudentCommunity(ctx, personID, exerciseID)
	if err != nil || communityID == 0 {
		return nil, err
	}
	p, err := st.progressOf(ctx, personID, communityID, subject{col: "exercise_id", id: exerciseID})
	if err != nil {
		return nil, err
	}
	if e, err := st.GetExercise(ctx, communityID, exerciseID); err == nil {
		for k, v := range e.Messages {
			if v != "" {
				p.Settings.Messages[k] = v
			}
		}
		if p.Level == "" && e.Level != "" {
			p.Level = e.Level
		}
	}
	return p, nil
}

func (st *Store) progressOf(ctx context.Context, personID, communityID int64, sub subject) (*SongProgress, error) {
	p := &SongProgress{Takes: []TakeScore{}}
	var level *string
	var pt, rt *int
	var reached *time.Time
	err := st.db.QueryRow(ctx, `SELECT a.level, a.pitch_target, a.rhythm_target, a.reached_at FROM practice_assignments a WHERE a.id = `+openAssignment(sub.col),
		personID, communityID, sub.id).Scan(&level, &pt, &rt, &reached)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if level != nil {
		p.Level = *level
	}
	if pt != nil {
		p.PitchTarget = *pt
	}
	if rt != nil {
		p.RhythmTarget = *rt
	}
	if (pt != nil || rt != nil) && p.Level == "" {
		p.Level = "beginner"
	}
	p.Reached = reached != nil
	var best TakeScore
	err = st.db.QueryRow(ctx, `
		SELECT pitch, rhythm, created_at FROM practice_takes
		WHERE person_id = $1 AND `+sub.col+` = $2 AND ($3 = '' OR level = $3)
		ORDER BY pitch + rhythm DESC, created_at DESC LIMIT 1`, personID, sub.id, p.Level).Scan(&best.Pitch, &best.Rhythm, &best.At)
	if err == nil {
		p.Best = &best
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	rows, err := st.db.Query(ctx, `
		SELECT pitch, rhythm, created_at FROM (
		  SELECT pitch, rhythm, created_at FROM practice_takes WHERE person_id = $1 AND `+sub.col+` = $2
		  ORDER BY created_at DESC LIMIT 20) t ORDER BY created_at`, personID, sub.id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tk TakeScore
		if err := rows.Scan(&tk.Pitch, &tk.Rhythm, &tk.At); err != nil {
			rows.Close()
			return nil, err
		}
		p.Takes = append(p.Takes, tk)
	}
	rows.Close()
	if p.Streak, err = st.PracticeStreak(ctx, communityID, personID); err != nil {
		return nil, err
	}
	if p.Settings, err = st.GetCheckSettings(ctx, communityID); err != nil {
		return nil, err
	}
	var name string
	if err = st.db.QueryRow(ctx, `SELECT display_name FROM people WHERE id = $1`, personID).Scan(&name); err != nil {
		return nil, err
	}
	p.Name, p.Coach = firstName(name), firstName(st.CoachName(ctx, communityID))
	return p, nil
}

// PracticeStreak is how many days in a row a student practised in a
// community, up to today (or yesterday, today not over yet).
func (st *Store) PracticeStreak(ctx context.Context, communityID, personID int64) (int, error) {
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT practiced_on FROM practice_logs WHERE community_id = $1 AND person_id = $2 AND practiced_on > current_date - 400
		ORDER BY practiced_on DESC`, communityID, personID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var days []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return 0, err
		}
		days = append(days, d)
	}
	if err := rows.Err(); err != nil || len(days) == 0 {
		return 0, err
	}
	var today time.Time
	if err := st.db.QueryRow(ctx, `SELECT current_date`).Scan(&today); err != nil {
		return 0, err
	}
	gap := int(today.Sub(days[0]).Hours() / 24)
	if gap > 1 {
		return 0, nil
	}
	n := 1
	for i := 1; i < len(days) && int(days[i-1].Sub(days[i]).Hours()/24) == 1; i++ {
		n++
	}
	return n, nil
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
		       coalesce(nullif(a.title, ''), s.title, ''), l.practiced_on::timestamptz, l.seconds, l.source, l.note,
		       l.pitch_takes, l.pitch_score, l.rhythm_score
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
		if err := rows.Scan(&l.ID, &l.AssignmentID, &l.VariantID, &l.Label, &l.PracticedOn, &l.Seconds, &l.Source, &l.Note,
			&l.PitchTakes, &l.PitchScore, &l.RhythmScore); err != nil {
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

// firstName is a name's first word ("Chloé Paul": "Chloé").
func firstName(s string) string {
	for i, r := range s {
		if r == ' ' {
			return s[:i]
		}
	}
	return s
}

// CoachName is a coaching community's coach's name (its first staff
// member's), else the community's.
func (st *Store) CoachName(ctx context.Context, communityID int64) string {
	var name string
	st.db.QueryRow(ctx, `
		SELECT coalesce((SELECT p.display_name FROM community_members cm JOIN people p ON p.id = cm.person_id
		                 JOIN community_roles r ON r.id = cm.role_id
		                 WHERE cm.community_id = $1 AND upper_inf(cm.validity) AND r.name = 'staff' ORDER BY cm.id LIMIT 1),
		                (SELECT name FROM communities WHERE id = $1), '')`, communityID).Scan(&name)
	return name
}
