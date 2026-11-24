package httpapi

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"app/internal/store"
)

// handleInvitePage shows a pending invite: if the visitor is already
// logged in, a "join as {them}" confirm button; otherwise a short
// create-account form, and a way to log in to an account they already
// have (handleInviteLoginRedirect). Every path posts to the same
// handlePostInvite. A personal invite (Kind() == "personal") also shows
// every membership already granted to its placeholder (shell) person —
// see store.GetPersonalInvite —: a new account finishes THAT placeholder
// (ClaimPersonalInvite, setting its email), an existing one takes its
// place (ClaimPersonalInviteAs). Joining is by email either way
// (docs/accounts.md): the inviter only ever typed a name.
func (s *Server) handleInvitePage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	inv, err := s.store.GetInvite(r.Context(), token)
	if err != nil {
		http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
		return
	}
	var loggedInAs string
	if p := s.sessionPerson(r); p != nil {
		loggedInAs = p.DisplayName
	}

	if inv.Kind() == "personal" {
		pv, err := s.store.GetPersonalInvite(r.Context(), token)
		if err != nil {
			http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
			return
		}
		s.renderPersonalInvitePage(w, pv, loggedInAs, "", "")
		return
	}

	s.renderInvitePage(w, inv, loggedInAs, "", "", "")
}

// sessionPerson is the visitor's account from their session cookie, nil
// without one: the invite and join pages are public, outside
// requireAuth/optionalPerson.
func (s *Server) sessionPerson(r *http.Request) *store.Person {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	p, err := s.store.GetSession(r.Context(), cookie.Value)
	if err != nil {
		return nil
	}
	return p
}

// handleInviteLoginRedirect is an invite's "I already have an account":
// remember the invite (pendingJoinCookie) and send them to log in, by
// password or by an emailed link; handleIndex accepts it once they're
// back (resumePendingJoin).
func (s *Server) handleInviteLoginRedirect(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.GetInvite(r.Context(), r.PathValue("token")); err != nil {
		http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
		return
	}
	s.loginThenResume(w, r, pendingInvite+r.PathValue("token"))
}

// renderInvitePage is the shared render for both the initial GET and a
// failed POST — the latter needs the exact same invite context
// (Kind/ProjectName/Role/BandName/Token) PLUS whatever the visitor
// already typed (so a rejected email/weak password doesn't make them
// retype their name too) and a reason the form bounced. formErr is
// empty on the normal first view.
func (s *Server) renderInvitePage(w http.ResponseWriter, inv *store.Invite, loggedInAs, formErr, displayName, email string) {
	s.render(w, "invite", map[string]any{
		"Token": inv.Token, "Kind": inv.Kind(),
		"BandName": inv.BandName, "BandStatus": inv.BandStatus,
		"LoggedInAs":  loggedInAs,
		"Error":       formErr,
		"DisplayName": displayName, "Email": email,
	})
}

// renderPersonalInvitePage is renderInvitePage's counterpart for a
// personal invite: reuses the same "invite" template (Kind == "personal"
// switches its content), but carries the pre-set membership list instead
// of a single band/role pair.
func (s *Server) renderPersonalInvitePage(w http.ResponseWriter, pv *store.PersonalInviteView, loggedInAs, formErr, email string) {
	s.render(w, "invite", map[string]any{
		"Token": pv.Token, "Kind": "personal",
		"DisplayName": pv.DisplayName, "Memberships": pv.Memberships,
		"LoggedInAs": loggedInAs,
		"Error":      formErr,
		"Email":      email,
	})
}

// handlePostInvite accepts an invite. A logged-in visitor just gets the
// membership added to their existing account; an anonymous one must
// supply a display name/email/password, which creates a
// brand-new account in the same transaction as the membership grant —
// see store.CreatePersonAndAcceptInvite. A password is REQUIRED here
// (unlike the account this account started as capable of — the store
// layer still accepts a blank hash for a magic-link-only account, kept
// for the --set-password-style bootstrap paths): asking a brand-new
// visitor to create one real credential up front, rather than defaulting
// them into "wait for an email every time," is the stronger default for
// an account they're about to use regularly — a password can always be
// changed later from /profile, same as every other account.
//
// Accepting the invite IS this account's first authentication either
// way: the visitor ends up logged in (a fresh session cookie) and routed
// into the getting-started wizard, which opens on its passkey-setup step
// (see handleWelcomePage) before anything else — a brand-new account is
// the ideal moment to offer "and while you're here, add a passkey too,"
// not a separate flow bolted on afterward.
func (s *Server) handlePostInvite(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	inv, invErr := s.store.GetInvite(r.Context(), token)
	if invErr != nil {
		http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
		return
	}
	if inv.Kind() == "personal" {
		s.handlePostPersonalInvite(w, r, token)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderInvitePage(w, inv, "", "invalid form", "", "")
		return
	}

	if p := s.sessionPerson(r); p != nil {
		dest, err := s.store.AcceptInvite(r.Context(), token, p.ID, p.DisplayName)
		if err != nil {
			http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
			return
		}
		http.Redirect(w, r, dest, http.StatusFound)
		return
	}

	displayName := strings.TrimSpace(r.FormValue("display_name"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	if displayName == "" || email == "" {
		s.renderInvitePage(w, inv, "", "display name and email are both required", displayName, email)
		return
	}
	// Same minimal shape check as the profile page's own email field
	// (handleSetEmailProfile) — not a strict RFC validator, just enough
	// to catch a typo before it becomes an account nobody can log into.
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		s.renderInvitePage(w, inv, "", "enter a valid email address", displayName, email)
		return
	}
	if len(password) < 8 {
		s.renderInvitePage(w, inv, "", "password must be at least 8 characters", displayName, email)
		return
	}
	if password != confirm {
		s.renderInvitePage(w, inv, "", "passwords didn't match", displayName, email)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hashing invite-accept password: %v", err)
		s.renderInvitePage(w, inv, "", "couldn't accept this invite — try again", displayName, email)
		return
	}

	personID, dest, err := s.store.CreatePersonAndAcceptInvite(r.Context(), token, displayName, email, string(hash))
	if err != nil {
		switch {
		case errors.Is(err, store.ErrEmailTaken):
			s.renderInvitePage(w, inv, "", "that email is already registered to another account", displayName, "")
		default:
			log.Printf("accepting invite %s: %v", token, err)
			s.renderInvitePage(w, inv, "", "couldn't accept this invite — try again", displayName, email)
		}
		return
	}
	sessToken, err := s.store.CreateSession(r.Context(), personID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sessToken, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: 30 * 24 * 60 * 60,
	})
	// This call only ever reaches a brand-new account (personID just came
	// out of CreatePersonAndAcceptInvite above) — offer the
	// getting-started wizard, STARTING on its passkey step (2), carrying
	// the real destination for its "Done"/"skip the rest" step so the
	// invite's own target isn't lost.
	http.Redirect(w, r, "/welcome?step=2&next="+url.QueryEscape(dest), http.StatusFound)
}

// handlePostPersonalInvite takes a placeholder's personal link. Someone
// signed in joins as themselves: the placeholder merges into their
// account (ClaimPersonalInviteAs) — the page asked them first, "Join as
// …?", with a way out if this browser isn't theirs. Anyone else makes
// their account with an email and a password, the same shape as
// handlePostInvite's anonymous branch (the name is the one the inviter
// gave; it can be changed later on /profile), on the EXISTING placeholder
// row (ClaimPersonalInvite: the email is set there); someone whose email
// has an account already is told to log in to join instead.
func (s *Server) handlePostPersonalInvite(w http.ResponseWriter, r *http.Request, token string) {
	pv, pvErr := s.store.GetPersonalInvite(r.Context(), token)
	if pvErr != nil {
		http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
		return
	}
	if p := s.sessionPerson(r); p != nil {
		if err := s.store.ClaimPersonalInviteAs(r.Context(), token, p.ID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
				return
			}
			log.Printf("joining as person %d with personal invite: %v", p.ID, err)
			s.renderPersonalInvitePage(w, pv, p.DisplayName, "couldn't join just now — try again", "")
			return
		}
		http.Redirect(w, r, personalInviteDest(pv), http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderPersonalInvitePage(w, pv, "", "invalid form", "")
		return
	}

	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	if email == "" {
		s.renderPersonalInvitePage(w, pv, "", "enter a valid email address", email)
		return
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		s.renderPersonalInvitePage(w, pv, "", "enter a valid email address", email)
		return
	}
	// Password is REQUIRED here, not an optional fallback to "log in with
	// an emailed link instead" — this deployment has no real SMTP wired up
	// (internal/mailer/console.go only logs to stdout), so a magic-link
	// login is not actually reachable by the account holder. Same
	// requirement as handlePostInvite's brand-new-account branch; the
	// passkey offered right after (via /welcome?step=2) is step two, not a
	// substitute for having a working credential at all.
	if len(password) < 8 {
		s.renderPersonalInvitePage(w, pv, "", "password must be at least 8 characters", email)
		return
	}
	if password != confirm {
		s.renderPersonalInvitePage(w, pv, "", "passwords didn't match", email)
		return
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hashing personal-invite password: %v", err)
		s.renderPersonalInvitePage(w, pv, "", "couldn't finish setting up this account — try again", email)
		return
	}
	hash := string(h)

	personID, err := s.store.ClaimPersonalInvite(r.Context(), token, email, hash)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrEmailTaken):
			s.renderPersonalInvitePage(w, pv, "", "that email is already registered to another account", "")
		default:
			log.Printf("claiming personal invite %s: %v", token, err)
			s.renderPersonalInvitePage(w, pv, "", "couldn't finish setting up this account — try again", email)
		}
		return
	}
	sessToken, err := s.store.CreateSession(r.Context(), personID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sessToken, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: 30 * 24 * 60 * 60,
	})
	http.Redirect(w, r, "/welcome?step=2&next=/home", http.StatusFound)
}

// personalInviteDest is where someone lands after taking a personal
// link: the first band or community the placeholder was in, else home.
func personalInviteDest(pv *store.PersonalInviteView) string {
	for _, m := range pv.Memberships {
		switch m.Kind {
		case "band":
			return fmt.Sprintf("/bands/%d", m.ID)
		case "community":
			return fmt.Sprintf("/communities/%d", m.ID)
		}
	}
	return "/home"
}
