// Package mailer defines a small, pluggable interface for sending
// outbound email (currently just magic-link login mail), plus one
// concrete implementation — ConsoleMailer — that stands in for a real
// SMTP provider in an environment with none. Swapping in a real
// provider later means implementing Mailer once more and changing what
// cmd/songbooks/main.go constructs; nothing else in the codebase should
// need to change.
package mailer

import "context"

// Mailer sends one email. Implementations decide what "sending" means —
// ConsoleMailer logs it and records it in the outbox table rather than
// making a network call.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}
