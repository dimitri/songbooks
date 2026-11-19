package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Warm-ups (migration 0087, docs/coaching.md): a coaching community's
// exercises, each a pattern written out in every key by the browser
// (warmup.js), the coach's recording of it in its written key, each
// student's path of keys, and sessions.

// Exercise is a warm-up exercise.
type Exercise struct {
	ID          int64             `json:"id"`
	CommunityID int64             `json:"communityId"`
	Name        string            `json:"name"`
	Kind        string            `json:"kind"`    // "pitch" or "rhythm"
	Pattern     json.RawMessage   `json:"pattern"` // warmup.js's
	Notes       string            `json:"notes"`
	Position    int               `json:"position"`
	Level       string            `json:"level,omitempty"`
	Messages    map[string]string `json:"messages"`
	HasDemo     bool              `json:"hasDemo"`
	DemoAt      *time.Time        `json:"demoAt,omitempty"`
}

// ExerciseInput is what the coach writes.
type ExerciseInput struct {
	Name     string            `json:"name"`
	Kind     string            `json:"kind"`
	Pattern  json.RawMessage   `json:"pattern"`
	Notes    string            `json:"notes"`
	Level    string            `json:"level"`
	Messages map[string]string `json:"messages"`
}

// ErrBadExercise is an exercise without a name or a pattern.
var ErrBadExercise = errors.New("an exercise needs a name and a pattern")

func (in *ExerciseInput) clean() error {
	if in.Name == "" || len(in.Pattern) == 0 || !json.Valid(in.Pattern) || len(in.Pattern) > 8000 {
		return ErrBadExercise
	}
	if in.Kind != "rhythm" {
		in.Kind = "pitch"
	}
	if in.Messages == nil {
		in.Messages = map[string]string{}
	}
	return nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// CreateExercise adds an exercise at the end of the community's.
func (st *Store) CreateExercise(ctx context.Context, communityID int64, in ExerciseInput, by int64) (int64, error) {
	if err := in.clean(); err != nil {
		return 0, err
	}
	if !takeLevels[in.Level] {
		in.Level = ""
	}
	msgs, _ := json.Marshal(in.Messages)
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO exercises (community_id, name, kind, pattern, notes, level, messages, position, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, coalesce((SELECT max(position) + 1 FROM exercises WHERE community_id = $1), 0), $8)
		RETURNING id`, communityID, in.Name, in.Kind, in.Pattern, in.Notes, nilIfEmpty(in.Level), msgs, by).Scan(&id)
	return id, err
}

// UpdateExercise changes an exercise of the community.
func (st *Store) UpdateExercise(ctx context.Context, communityID, id int64, in ExerciseInput) error {
	if err := in.clean(); err != nil {
		return err
	}
	if !takeLevels[in.Level] {
		in.Level = ""
	}
	msgs, _ := json.Marshal(in.Messages)
	tag, err := st.db.Exec(ctx, `
		UPDATE exercises SET name = $3, kind = $4, pattern = $5, notes = $6, level = $7, messages = $8, updated_at = now()
		WHERE id = $1 AND community_id = $2`, id, communityID, in.Name, in.Kind, in.Pattern, in.Notes, nilIfEmpty(in.Level), msgs)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// DeleteExercise removes an exercise of the community.
func (st *Store) DeleteExercise(ctx context.Context, communityID, id int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM exercises WHERE id = $1 AND community_id = $2`, id, communityID)
	return err
}

const exerciseCols = `id, community_id, name, kind, pattern, notes, position, coalesce(level, ''), messages, demo IS NOT NULL, demo_at`

func scanExercise(row pgx.Row) (*Exercise, error) {
	var e Exercise
	var msgs []byte
	if err := row.Scan(&e.ID, &e.CommunityID, &e.Name, &e.Kind, &e.Pattern, &e.Notes, &e.Position, &e.Level, &msgs, &e.HasDemo, &e.DemoAt); err != nil {
		return nil, err
	}
	e.Messages = map[string]string{}
	json.Unmarshal(msgs, &e.Messages)
	return &e, nil
}

// GetExercise is an exercise of the community.
func (st *Store) GetExercise(ctx context.Context, communityID, id int64) (*Exercise, error) {
	e, err := scanExercise(st.db.QueryRow(ctx, `SELECT `+exerciseCols+` FROM exercises WHERE id = $1 AND community_id = $2`, id, communityID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// ListExercises is a community's exercises, in order.
func (st *Store) ListExercises(ctx context.Context, communityID int64) ([]Exercise, error) {
	rows, err := st.db.Query(ctx, `SELECT `+exerciseCols+` FROM exercises WHERE community_id = $1 ORDER BY position, id`, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Exercise
	for rows.Next() {
		e, err := scanExercise(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// SetExerciseDemo keeps the coach's recording of an exercise (nil:
// removed).
func (st *Store) SetExerciseDemo(ctx context.Context, communityID, id int64, audio []byte, contentType string) error {
	var at any
	if audio != nil {
		at = time.Now()
	}
	tag, err := st.db.Exec(ctx, `UPDATE exercises SET demo = $3, demo_type = $4, demo_at = $5 WHERE id = $1 AND community_id = $2`,
		id, communityID, audio, contentType, at)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ExerciseDemo is the coach's recording of an exercise (nil: none).
func (st *Store) ExerciseDemo(ctx context.Context, communityID, id int64) ([]byte, string, error) {
	var audio []byte
	var ct string
	err := st.db.QueryRow(ctx, `SELECT demo, demo_type FROM exercises WHERE id = $1 AND community_id = $2`, id, communityID).Scan(&audio, &ct)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return audio, ct, err
}

// ExercisePath is a student's waypoints through an exercise (nil: the
// app's, from their range).
func (st *Store) ExercisePath(ctx context.Context, exerciseID, personID int64) ([]int, error) {
	var w []int32
	err := st.db.QueryRow(ctx, `SELECT waypoints FROM exercise_paths WHERE exercise_id = $1 AND person_id = $2`, exerciseID, personID).Scan(&w)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	out := make([]int, len(w))
	for i, v := range w {
		out[i] = int(v)
	}
	return out, err
}

// SetExercisePath sets a student's waypoints (±24 semitones, at most 12;
// none: back to the app's).
func (st *Store) SetExercisePath(ctx context.Context, exerciseID, personID int64, waypoints []int) error {
	if len(waypoints) == 0 {
		_, err := st.db.Exec(ctx, `DELETE FROM exercise_paths WHERE exercise_id = $1 AND person_id = $2`, exerciseID, personID)
		return err
	}
	if len(waypoints) > 12 {
		waypoints = waypoints[:12]
	}
	for i, v := range waypoints {
		waypoints[i] = max(-24, min(24, v))
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO exercise_paths (exercise_id, person_id, waypoints) VALUES ($1, $2, $3)
		ON CONFLICT (exercise_id, person_id) DO UPDATE SET waypoints = excluded.waypoints`, exerciseID, personID, waypoints)
	return err
}

// StudentRangeSpan is a student's whole range, lowest to highest note
// over their registers (0, 0: none noted).
func (st *Store) StudentRangeSpan(ctx context.Context, communityID, personID int64) (low, high int, err error) {
	rs, err := st.StudentRanges(ctx, communityID, personID)
	if err != nil {
		return 0, 0, err
	}
	for _, r := range rs {
		if r.Low != nil && (low == 0 || *r.Low < low) {
			low = *r.Low
		}
		if r.High != nil && *r.High > high {
			high = *r.High
		}
	}
	return low, high, nil
}

// ExerciseSession is a session: exercises played through as one.
type ExerciseSession struct {
	ID          int64   `json:"id"`
	CommunityID int64   `json:"communityId"`
	Name        string  `json:"name"`
	ExerciseIDs []int64 `json:"exerciseIds"`
}

// ListExerciseSessions is a community's sessions, in order.
func (st *Store) ListExerciseSessions(ctx context.Context, communityID int64) ([]ExerciseSession, error) {
	rows, err := st.db.Query(ctx, `SELECT id, community_id, name, exercise_ids FROM exercise_sessions WHERE community_id = $1 ORDER BY position, id`, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExerciseSession
	for rows.Next() {
		var s ExerciseSession
		if err := rows.Scan(&s.ID, &s.CommunityID, &s.Name, &s.ExerciseIDs); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetExerciseSession is a session of the community.
func (st *Store) GetExerciseSession(ctx context.Context, communityID, id int64) (*ExerciseSession, error) {
	var s ExerciseSession
	err := st.db.QueryRow(ctx, `SELECT id, community_id, name, exercise_ids FROM exercise_sessions WHERE id = $1 AND community_id = $2`, id, communityID).
		Scan(&s.ID, &s.CommunityID, &s.Name, &s.ExerciseIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, err
}

// SaveExerciseSession creates (id 0) or changes a session; its exercises
// must be the community's.
func (st *Store) SaveExerciseSession(ctx context.Context, communityID, id int64, name string, exerciseIDs []int64) (int64, error) {
	if name == "" || len(exerciseIDs) == 0 {
		return 0, errors.New("a session needs a name and exercises")
	}
	var ok bool
	if err := st.db.QueryRow(ctx, `SELECT count(*) = cardinality($2::bigint[]) FROM exercises WHERE community_id = $1 AND id = ANY($2)`,
		communityID, exerciseIDs).Scan(&ok); err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("not this community's exercises")
	}
	if id == 0 {
		err := st.db.QueryRow(ctx, `
			INSERT INTO exercise_sessions (community_id, name, exercise_ids, position)
			VALUES ($1, $2, $3, coalesce((SELECT max(position) + 1 FROM exercise_sessions WHERE community_id = $1), 0)) RETURNING id`,
			communityID, name, exerciseIDs).Scan(&id)
		return id, err
	}
	tag, err := st.db.Exec(ctx, `UPDATE exercise_sessions SET name = $3, exercise_ids = $4 WHERE id = $1 AND community_id = $2`, id, communityID, name, exerciseIDs)
	if err == nil && tag.RowsAffected() == 0 {
		return 0, ErrNotFound
	}
	return id, err
}

// DeleteExerciseSession removes a session of the community.
func (st *Store) DeleteExerciseSession(ctx context.Context, communityID, id int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM exercise_sessions WHERE id = $1 AND community_id = $2`, id, communityID)
	return err
}
