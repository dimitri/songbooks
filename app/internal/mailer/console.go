package mailer

import (
	"context"
	"log"
)

// outboxStore is the subset of *store.Store ConsoleMailer needs — kept
// as a tiny local interface rather than importing app/internal/store
// directly, so this package has no dependency on the store package at
// all (and so a future unit test can stub it trivially).
type outboxStore interface {
	InsertOutbox(ctx context.Context, to, subject, body string) error
}

// ConsoleMailer is the dev-mode stand-in for a real SMTP mailer: it logs
// the full email (including any magic-link URL in the body) to server
// stdout, and also inserts a durable row into the outbox table so it can
// be read back later from GET /dev/outbox without tailing logs. This is
// the only Mailer implementation this phase ships — see
// cmd/songbooks/main.go's --smtp-url flag stub for how a real one would
// slot in later.
type ConsoleMailer struct {
	store outboxStore
}

// NewConsoleMailer builds a ConsoleMailer backed by st for durable
// outbox storage.
func NewConsoleMailer(st outboxStore) *ConsoleMailer {
	return &ConsoleMailer{store: st}
}

func (m *ConsoleMailer) Send(ctx context.Context, to, subject, body string) error {
	log.Printf("[mailer] to=%s subject=%q\n%s", to, subject, body)
	return m.store.InsertOutbox(ctx, to, subject, body)
}
