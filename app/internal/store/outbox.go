package store

import (
	"context"
	"time"
)

// OutboxEntry is one row of the dev-mode mailer's durable record of
// "emails" it sent — see internal/mailer.ConsoleMailer and
// GET /dev/outbox.
type OutboxEntry struct {
	ID        int64
	To        string
	Subject   string
	Body      string
	CreatedAt time.Time
}

// InsertOutbox records one sent (console-logged, really) email.
func (st *Store) InsertOutbox(ctx context.Context, to, subject, body string) error {
	_, err := st.db.Exec(ctx,
		`INSERT INTO outbox (to_email, subject, body) VALUES ($1, $2, $3)`,
		to, subject, body)
	return err
}

// ListOutbox returns the most recent outbox entries, newest first, for
// GET /dev/outbox.
func (st *Store) ListOutbox(ctx context.Context, limit int) ([]OutboxEntry, error) {
	rows, err := st.db.Query(ctx,
		`SELECT id, to_email, subject, body, created_at FROM outbox ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxEntry
	for rows.Next() {
		var e OutboxEntry
		if err := rows.Scan(&e.ID, &e.To, &e.Subject, &e.Body, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
