package httpapi

import "unicode/utf8"

// The password rule, the same everywhere a password is set: every way
// to create an account or claim a placeholder (an invite or personal
// link, a band or community link, registering for an event) and the
// profile page. No login email is actually sent yet (the console mailer:
// --smtp-url isn't built), so a new account needs a password, or it is
// locked out once its session ends (docs/accounts.md).
//
// At least minPasswordLen characters; at most maxPasswordBytes bytes,
// bcrypt's own limit (it refuses a longer one). Nothing else: no
// character classes.
const (
	minPasswordLen   = 8
	maxPasswordBytes = 72
)

// newPasswordProblem says what's wrong with a new password and its
// confirmation, "" when nothing is. The reasons are lower-case
// one-liners the pages recognise and move under the field they're about
// (invite.html, join.html, event.html, profile.html).
func newPasswordProblem(password, confirm string) string {
	switch {
	case password == "":
		return "a password is required"
	case utf8.RuneCountInString(password) < minPasswordLen:
		return "password must be at least 8 characters"
	case len(password) > maxPasswordBytes:
		return "password is too long (72 bytes at most)"
	case password != confirm:
		return "passwords didn't match"
	}
	return ""
}
