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

// Delivering is implemented by a Mailer that really delivers mail to
// people's inboxes (an SMTP one, once --smtp-url is built). The console
// mailer doesn't: its login emails only reach the server log and the
// outbox table.
type Delivering interface {
	Delivers() bool
}

// Delivers says whether m's emails reach people. Without that, an
// emailed login link isn't a way back into an account, so the login
// page puts the password first and every new account needs one
// (docs/accounts.md).
func Delivers(m Mailer) bool {
	d, ok := m.(Delivering)
	return ok && d.Delivers()
}
