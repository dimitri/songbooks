package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// A coaching community's groups (migration 0080, docs/coaching.md): a
// year's class, or a one-off around an event (a master class, a stage).
// A group has its songbook (kind "group", its members granted), each song
// in the group's key, and its lessons (events of type "lesson" for the
// group, its members registered).

// CoachingGroup is a group and its members.
type CoachingGroup struct {
	ID          int64
	CommunityID int64
	Name        string
	Kind        string // "year" or "oneoff"
	StartsOn    *time.Time
	EndsOn      *time.Time
	EventID     *int64
	SongbookID  int64
	Members     []GroupMember
}

// IsYear reports whether the group works together for a year.
func (g CoachingGroup) IsYear() bool { return g.Kind == "year" }

// HasMember reports whether personID is one of the group's members.
func (g CoachingGroup) HasMember(personID int64) bool {
	for _, m := range g.Members {
		if m.PersonID == personID {
			return true
		}
	}
	return false
}

// GroupMember is one student of a group.
type GroupMember struct {
	PersonID    int64
	DisplayName string
}

// CreateCoachingGroup makes a group and its songbook.
func (st *Store) CreateCoachingGroup(ctx context.Context, communityID int64, name, kind string, startsOn, endsOn *time.Time, eventID *int64) (int64, error) {
	if kind != "year" && kind != "oneoff" {
		return 0, fmt.Errorf("invalid group kind %q", kind)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var sbID, id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO songbooks (owner_kind, owner_community_id, name, is_default, kind, requires_grant)
		VALUES ('community', $1, $2, false, 'group', true) RETURNING id`, communityID, name).Scan(&sbID); err != nil {
		return 0, err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO coaching_groups (community_id, name, kind, starts_on, ends_on, event_id, songbook_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		communityID, name, kind, startsOn, endsOn, eventID, sbID).Scan(&id); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// UpdateCoachingGroup changes a group's name and dates.
func (st *Store) UpdateCoachingGroup(ctx context.Context, groupID int64, name string, startsOn, endsOn *time.Time) error {
	_, err := st.db.Exec(ctx, `UPDATE coaching_groups SET name = $2, starts_on = $3, ends_on = $4 WHERE id = $1`, groupID, name, startsOn, endsOn)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(ctx, `UPDATE songbooks SET name = $2 WHERE id = (SELECT songbook_id FROM coaching_groups WHERE id = $1)`, groupID, name)
	return err
}

// DeleteCoachingGroup removes a group and its songbook (its lessons stay,
// for no group).
func (st *Store) DeleteCoachingGroup(ctx context.Context, groupID int64) error {
	_, err := st.db.Exec(ctx, `
		WITH g AS (DELETE FROM coaching_groups WHERE id = $1 RETURNING songbook_id)
		DELETE FROM songbooks WHERE id IN (SELECT songbook_id FROM g)`, groupID)
	return err
}

// ListCoachingGroups is a community's groups, the year's first, then by
// start; with personID, only that student's.
func (st *Store) ListCoachingGroups(ctx context.Context, communityID, personID int64) ([]CoachingGroup, error) {
	rows, err := st.db.Query(ctx, `
		SELECT g.id, g.community_id, g.name, g.kind, g.starts_on, g.ends_on, g.event_id, coalesce(g.songbook_id, 0)
		FROM coaching_groups g
		WHERE g.community_id = $1
		  AND ($2 = 0 OR EXISTS (SELECT 1 FROM coaching_group_members m WHERE m.group_id = g.id AND m.person_id = $2))
		ORDER BY (g.kind <> 'year'), g.starts_on DESC NULLS LAST, g.name`, communityID, personID)
	if err != nil {
		return nil, err
	}
	var out []CoachingGroup
	for rows.Next() {
		var g CoachingGroup
		if err := rows.Scan(&g.ID, &g.CommunityID, &g.Name, &g.Kind, &g.StartsOn, &g.EndsOn, &g.EventID, &g.SongbookID); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Members, err = st.groupMembers(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// GetCoachingGroup is one group, with its members.
func (st *Store) GetCoachingGroup(ctx context.Context, groupID int64) (*CoachingGroup, error) {
	var g CoachingGroup
	err := st.db.QueryRow(ctx, `
		SELECT id, community_id, name, kind, starts_on, ends_on, event_id, coalesce(songbook_id, 0)
		FROM coaching_groups WHERE id = $1`, groupID).
		Scan(&g.ID, &g.CommunityID, &g.Name, &g.Kind, &g.StartsOn, &g.EndsOn, &g.EventID, &g.SongbookID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	g.Members, err = st.groupMembers(ctx, groupID)
	return &g, err
}

func (st *Store) groupMembers(ctx context.Context, groupID int64) ([]GroupMember, error) {
	rows, err := st.db.Query(ctx, `
		SELECT m.person_id, p.display_name FROM coaching_group_members m
		JOIN people p ON p.id = m.person_id WHERE m.group_id = $1 ORDER BY p.display_name`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupMember
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.PersonID, &m.DisplayName); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetGroupMembers makes people the group's members: they get its songbook,
// and its coming lessons (registered); who leaves loses both.
func (st *Store) SetGroupMembers(ctx context.Context, groupID int64, people []int64, byPersonID int64) error {
	g, err := st.GetCoachingGroup(ctx, groupID)
	if err != nil {
		return err
	}
	want := map[int64]bool{}
	for _, p := range people {
		want[p] = true
	}
	for _, m := range g.Members {
		if want[m.PersonID] {
			delete(want, m.PersonID)
			continue
		}
		if _, err := st.db.Exec(ctx, `DELETE FROM coaching_group_members WHERE group_id = $1 AND person_id = $2`, groupID, m.PersonID); err != nil {
			return err
		}
		if g.SongbookID != 0 {
			if err := st.RevokeSongbookAccess(ctx, g.SongbookID, m.PersonID); err != nil {
				return err
			}
		}
		if _, err := st.db.Exec(ctx, `
			DELETE FROM event_registrations WHERE person_id = $2 AND event_id IN (
				SELECT id FROM events WHERE coaching_group_id = $1 AND starts_at > now())`, groupID, m.PersonID); err != nil {
			return err
		}
	}
	for p := range want {
		if _, err := st.db.Exec(ctx, `INSERT INTO coaching_group_members (group_id, person_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, groupID, p); err != nil {
			return err
		}
		if g.SongbookID != 0 {
			if err := st.GrantSongbookAccess(ctx, g.SongbookID, p, byPersonID); err != nil {
				return err
			}
		}
		if _, err := st.db.Exec(ctx, `
			INSERT INTO event_registrations (event_id, person_id)
			SELECT id, $2 FROM events WHERE coaching_group_id = $1 AND starts_at > now()
			ON CONFLICT (event_id, person_id) DO NOTHING`, groupID, p); err != nil {
			return err
		}
	}
	return nil
}

// SetLessonDetails says how a lesson is given: in the room or online, and
// for which group (its members registered).
func (st *Store) SetLessonDetails(ctx context.Context, eventID int64, mode string, groupID *int64) error {
	if mode != "in_person" && mode != "online" {
		return fmt.Errorf("invalid lesson mode %q", mode)
	}
	_, err := st.db.Exec(ctx, `UPDATE events SET lesson_mode = $2, coaching_group_id = $3 WHERE id = $1`, eventID, mode, groupID)
	return err
}

// Teaching notes ------------------------------------------------------------

// TeachingNote is what the coach wrote about a student, on a day, maybe
// about a lesson.
type TeachingNote struct {
	ID         int64
	Body       string
	EventID    *int64
	EventName  string
	EventStart *time.Time
	AuthorName string
	CreatedAt  time.Time
}

// AddTeachingNote writes a note about a student.
func (st *Store) AddTeachingNote(ctx context.Context, communityID, personID int64, eventID *int64, body string, authorID int64) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO teaching_notes (community_id, person_id, event_id, body, author_person_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, communityID, personID, eventID, body, nullID(authorID)).Scan(&id)
	return id, err
}

// UpdateTeachingNote rewrites a note of a student's.
func (st *Store) UpdateTeachingNote(ctx context.Context, communityID, personID, noteID int64, body string) error {
	tag, err := st.db.Exec(ctx, `
		UPDATE teaching_notes SET body = $4, updated_at = now()
		WHERE id = $3 AND community_id = $1 AND person_id = $2`, communityID, personID, noteID, body)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// DeleteTeachingNote removes a note of a student's.
func (st *Store) DeleteTeachingNote(ctx context.Context, communityID, personID, noteID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM teaching_notes WHERE id = $3 AND community_id = $1 AND person_id = $2`, communityID, personID, noteID)
	return err
}

// ListTeachingNotes is a student's notes, the latest first.
func (st *Store) ListTeachingNotes(ctx context.Context, communityID, personID int64) ([]TeachingNote, error) {
	rows, err := st.db.Query(ctx, `
		SELECT n.id, n.body, n.event_id, coalesce(e.name, ''), e.starts_at, coalesce(p.display_name, ''), n.created_at
		FROM teaching_notes n
		LEFT JOIN events e ON e.id = n.event_id
		LEFT JOIN people p ON p.id = n.author_person_id
		WHERE n.community_id = $1 AND n.person_id = $2
		ORDER BY coalesce(e.starts_at, n.created_at) DESC, n.id DESC`, communityID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TeachingNote
	for rows.Next() {
		var n TeachingNote
		if err := rows.Scan(&n.ID, &n.Body, &n.EventID, &n.EventName, &n.EventStart, &n.AuthorName, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Calendar feeds ------------------------------------------------------------

// CalendarFeedToken is the secret of personID's calendar feed, made the
// first time.
func (st *Store) CalendarFeedToken(ctx context.Context, personID int64) (string, error) {
	var token string
	err := st.db.QueryRow(ctx, `SELECT token FROM calendar_feeds WHERE person_id = $1`, personID).Scan(&token)
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	return st.ResetCalendarFeed(ctx, personID)
}

// ResetCalendarFeed gives personID's feed a new secret: the old address
// stops working.
func (st *Store) ResetCalendarFeed(ctx context.Context, personID int64) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	_, err := st.db.Exec(ctx, `
		INSERT INTO calendar_feeds (person_id, token) VALUES ($1, $2)
		ON CONFLICT (person_id) DO UPDATE SET token = EXCLUDED.token, created_at = now()`, personID, token)
	return token, err
}

// PersonByCalendarToken is whose feed token is, 0 for none.
func (st *Store) PersonByCalendarToken(ctx context.Context, token string) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `SELECT person_id FROM calendar_feeds WHERE token = $1`, token).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}
