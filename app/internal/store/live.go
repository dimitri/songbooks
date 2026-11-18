package store

import (
	"context"
	"time"
)

// Live lessons (migration 0083, docs/live-lessons.md): an online lesson
// meets in its own room on Galène, lessons/<live_room>, the room made
// the first time someone joins.

// LiveRoom is an online lesson's room, made on its first call.
func (st *Store) LiveRoom(ctx context.Context, eventID int64) (string, error) {
	var room string
	err := st.db.QueryRow(ctx, `
		UPDATE events SET live_room = coalesce(live_room, gen_random_uuid())
		WHERE id = $1 RETURNING live_room::text`, eventID).Scan(&room)
	return room, err
}

// LiveHeadphones is whether a person joins live lessons wearing
// headphones.
func (st *Store) LiveHeadphones(ctx context.Context, personID int64) (bool, error) {
	var on bool
	err := st.db.QueryRow(ctx, `SELECT live_headphones FROM people WHERE id = $1`, personID).Scan(&on)
	return on, err
}

// SetLiveHeadphones remembers whether a person wears headphones.
func (st *Store) SetLiveHeadphones(ctx context.Context, personID int64, on bool) error {
	_, err := st.db.Exec(ctx, `UPDATE people SET live_headphones = $2 WHERE id = $1`, personID, on)
	return err
}

// Live lessons open this long before they start, and close this long
// after they end (or after an hour, with no end).
const (
	LiveOpensBefore = 15 * time.Minute
	LiveClosesAfter = 30 * time.Minute
)

// LiveWindow is when an online lesson's room can be joined.
func (e Event) LiveWindow() (opens, closes time.Time) {
	end := e.StartsAt.Add(time.Hour)
	if e.EndsAt != nil {
		end = *e.EndsAt
	}
	return e.StartsAt.Add(-LiveOpensBefore), end.Add(LiveClosesAfter)
}

// LiveOpen is whether an online lesson's room can be joined at now.
func (e Event) LiveOpen(now time.Time) bool {
	opens, closes := e.LiveWindow()
	return e.LessonMode == "online" && !now.Before(opens) && !now.After(closes)
}
