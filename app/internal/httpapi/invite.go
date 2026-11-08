package httpapi

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"app/internal/store"
)

// handleInvitePage shows a pending invite: if the visitor is already
// logged in, a "join as {them}" confirm button; otherwise a short
// create-account form. Either path posts to the same handlePostInvite.
// A personal invite (Kind() == "personal") instead shows every
// membership already granted to its shell person — see
// store.GetPersonalInvite — and its form only ever finishes THAT
// account (ClaimPersonalInvite), never creates a new one, so the
// "already logged in" branch doesn't apply to it at all.
func (s *Server) handleInvitePage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	inv, err := s.store.GetInvite(r.Context(), token)
	if err != nil {
		http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
		return
	}

	if inv.Kind() == "personal" {
		pv, err := s.store.GetPersonalInvite(r.Context(), token)
		if err != nil {
			http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
			return
		}
		s.renderPersonalInvitePage(w, pv, "", pv.DisplayName, "")
		return
	}

	var loggedInAs string
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if p, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
			loggedInAs = p.DisplayName
		}
	}

	s.renderInvitePage(w, inv, loggedInAs, "", "", "", "")
}

// renderInvitePage is the shared render for both the initial GET and a
// failed POST — the latter needs the exact same invite context
// (Kind/ProjectName/Role/BandName/Token) PLUS whatever the visitor
// already typed (so a rejected username/weak password doesn't make them
// retype their email too) and a reason the form bounced. formErr is
// empty on the normal first view.
func (s *Server) renderInvitePage(w http.ResponseWriter, inv *store.Invite, loggedInAs, formErr, username, displayName, email string) {
	s.render(w, "invite", map[string]any{
		"Token": inv.Token, "Kind": inv.Kind(),
		"BandName": inv.BandName, "BandStatus": inv.BandStatus,
		"LoggedInAs": loggedInAs,
		"Error":      formErr,
		"Username":   username, "DisplayName": displayName, "Email": email,
	})
}

// renderPersonalInvitePage is renderInvitePage's counterpart for a
// personal invite: reuses the same "invite" template (Kind == "personal"
// switches its content), but carries the pre-set membership list instead
// of a single band/role pair.
func (s *Server) renderPersonalInvitePage(w http.ResponseWriter, pv *store.PersonalInviteView, formErr, username, email string) {
	s.render(w, "invite", map[string]any{
		"Token": pv.Token, "Kind": "personal",
		"DisplayName": pv.DisplayName, "Memberships": pv.Memberships,
		"Error":    formErr,
		"Username": username, "Email": email,
	})
}

// handlePostInvite accepts an invite. A logged-in visitor just gets the
// membership added to their existing account; an anonymous one must
// supply a username/display name/email/password, which creates a
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
		s.renderInvitePage(w, inv, "", "invalid form", "", "", "")
		return
	}

	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if p, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
			dest, err := s.store.AcceptInvite(r.Context(), token, p.ID, p.DisplayName)
			if err != nil {
				http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
				return
			}
			http.Redirect(w, r, dest, http.StatusFound)
			return
		}
	}

	username := strings.TrimSpace(r.FormValue("username"))
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	if username == "" || displayName == "" || email == "" {
		s.renderInvitePage(w, inv, "", "username, display name and email are all required", username, displayName, email)
		return
	}
	// Same minimal shape check as the profile page's own email field
	// (handleSetEmailProfile) — not a strict RFC validator, just enough
	// to catch a typo before it becomes an account nobody can log into.
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		s.renderInvitePage(w, inv, "", "enter a valid email address", username, displayName, email)
		return
	}
	if len(password) < 8 {
		s.renderInvitePage(w, inv, "", "password must be at least 8 characters", username, displayName, email)
		return
	}
	if password != confirm {
		s.renderInvitePage(w, inv, "", "passwords didn't match", username, displayName, email)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hashing invite-accept password: %v", err)
		s.renderInvitePage(w, inv, "", "couldn't accept this invite — try again", username, displayName, email)
		return
	}

	personID, dest, err := s.store.CreatePersonAndAcceptInvite(r.Context(), token, username, displayName, email, string(hash))
	if err != nil {
		switch {
		case errors.Is(err, store.ErrUsernameTaken):
			s.renderInvitePage(w, inv, "", "that username is already taken — try another", "", displayName, email)
		case errors.Is(err, store.ErrEmailTaken):
			s.renderInvitePage(w, inv, "", "that email is already registered to another account", username, displayName, "")
		default:
			log.Printf("accepting invite %s: %v", token, err)
			s.renderInvitePage(w, inv, "", "couldn't accept this invite — try again", username, displayName, email)
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

// handlePostPersonalInvite finishes a shell person's account — same
// username/email/password shape as handlePostInvite's anonymous branch,
// but calls ClaimPersonalInvite (sets the fields on the EXISTING shell
// row) instead of CreatePersonAndAcceptInvite (which always inserts a
// new one). No "already logged in, just add membership" branch: a
// personal invite's whole point is finishing ONE specific pre-named
// account, not attaching its memberships to whoever happens to already
// be signed in on this browser.
func (s *Server) handlePostPersonalInvite(w http.ResponseWriter, r *http.Request, token string) {
	pv, pvErr := s.store.GetPersonalInvite(r.Context(), token)
	if pvErr != nil {
		http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderPersonalInvitePage(w, pv, "invalid form", "", "")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	if username == "" || email == "" {
		s.renderPersonalInvitePage(w, pv, "username and email are both required", username, email)
		return
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		s.renderPersonalInvitePage(w, pv, "enter a valid email address", username, email)
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
		s.renderPersonalInvitePage(w, pv, "password must be at least 8 characters", username, email)
		return
	}
	if password != confirm {
		s.renderPersonalInvitePage(w, pv, "passwords didn't match", username, email)
		return
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hashing personal-invite password: %v", err)
		s.renderPersonalInvitePage(w, pv, "couldn't finish setting up this account — try again", username, email)
		return
	}
	hash := string(h)

	personID, err := s.store.ClaimPersonalInvite(r.Context(), token, username, email, hash)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrUsernameTaken):
			s.renderPersonalInvitePage(w, pv, "that username is already taken — try another", "", email)
		case errors.Is(err, store.ErrEmailTaken):
			s.renderPersonalInvitePage(w, pv, "that email is already registered to another account", username, "")
		default:
			log.Printf("claiming personal invite %s: %v", token, err)
			s.renderPersonalInvitePage(w, pv, "couldn't finish setting up this account — try again", username, email)
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
