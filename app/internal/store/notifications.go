package store

import (
	"context"
	"time"
)

// Notification is one row of a person's in-app inbox — not email, not
// push, just something to check while logged in. Kind is a free-form tag
// ("band_invite_accepted", "mention", ...), deliberately not an enum (see
// migration 0020); Link is optional (empty means "informational only,
// nothing to navigate to").
type Notification struct {
	ID        int64
	PersonID  int64
	Kind      string
	Body      string
	Link      string
	ReadAt    *time.Time
	CreatedAt time.Time
}

// IsRead reports whether n has been marked read.
func (n Notification) IsRead() bool { return n.ReadAt != nil }

// CreateNotification files a new notification for personID. link may be
// "" for a notification with nothing to navigate to.
func (st *Store) CreateNotification(ctx context.Context, personID int64, kind, body, link string) (int64, error) {
	var id int64
	var linkArg any
	if link != "" {
		linkArg = link
	}
	err := st.db.QueryRow(ctx,
		`INSERT INTO notifications (person_id, kind, body, link) VALUES ($1, $2, $3, $4) RETURNING id`,
		personID, kind, body, linkArg).Scan(&id)
	return id, err
}

// ListNotifications lists personID's notifications, newest first. Pass
// unreadOnly=true for the badge/dropdown case where read ones shouldn't
// show.
func (st *Store) ListNotifications(ctx context.Context, personID int64, unreadOnly bool) ([]Notification, error) {
	q := `SELECT id, person_id, kind, body, coalesce(link, ''), read_at, created_at
		FROM notifications WHERE person_id = $1`
	if unreadOnly {
		q += ` AND read_at IS NULL`
	}
	q += ` ORDER BY created_at DESC`
	rows, err := st.db.Query(ctx, q, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.PersonID, &n.Kind, &n.Body, &n.Link, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkRead marks one of personID's own notifications read. Marking an
// already-read (or someone else's, or nonexistent) notification read is a
// no-op, not an error — same "idempotent, can't fail by repeating"
// reasoning as DeleteSession.
func (st *Store) MarkRead(ctx context.Context, notificationID, personID int64) error {
	_, err := st.db.Exec(ctx,
		`UPDATE notifications SET read_at = now() WHERE id = $1 AND person_id = $2 AND read_at IS NULL`,
		notificationID, personID)
	return err
}

// MarkAllRead marks every one of personID's unread notifications read.
func (st *Store) MarkAllRead(ctx context.Context, personID int64) error {
	_, err := st.db.Exec(ctx,
		`UPDATE notifications SET read_at = now() WHERE person_id = $1 AND read_at IS NULL`, personID)
	return err
}

// UnreadCount returns how many unread notifications personID has — the nav
// bell's badge count.
func (st *Store) UnreadCount(ctx context.Context, personID int64) (int, error) {
	var n int
	err := st.db.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE person_id = $1 AND read_at IS NULL`, personID).Scan(&n)
	return n, err
}
