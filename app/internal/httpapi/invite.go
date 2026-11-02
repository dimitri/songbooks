package httpapi

import (
	"log"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// handleInvitePage shows a pending invite: if the visitor is already
// logged in, a "join as {them}" confirm button; otherwise a short
// create-account form. Either path posts to the same handlePostInvite.
func (s *Server) handleInvitePage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	inv, err := s.store.GetInvite(r.Context(), token)
	if err != nil {
		http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
		return
	}

	var loggedInAs string
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if p, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
			loggedInAs = p.DisplayName
		}
	}

	s.render(w, "invite", map[string]any{
		"Token": inv.Token, "Kind": inv.Kind(),
		"ProjectName": inv.ProjectName, "Role": inv.Role,
		"BandName": inv.BandName, "BandStatus": inv.BandStatus,
		"LoggedInAs": loggedInAs,
	})
}

// handlePostInvite accepts an invite. A logged-in visitor just gets the
// membership added to their existing account; an anonymous one must supply
// a username/display name/email, which creates a brand-new account in the
// same transaction as the membership grant — see
// store.CreatePersonAndAcceptInvite. The form's password field is
// optional: left blank, the new account has no password and falls back to
// magic-link-only login, exactly like before password login was restored;
// filled in, it's hashed here and the account can log in with it right
// away. Accepting the invite IS this account's first authentication
// either way: the visitor ends up logged in (a fresh session cookie) and
// redirected into the project.
func (s *Server) handlePostInvite(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
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
	if username == "" || displayName == "" || email == "" {
		http.Error(w, "username, display name and email are all required", http.StatusBadRequest)
		return
	}

	var bcryptHash string
	if password := r.FormValue("password"); password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("hashing invite-accept password: %v", err)
			http.Error(w, "couldn't accept this invite", http.StatusInternalServerError)
			return
		}
		bcryptHash = string(hash)
	}

	personID, dest, err := s.store.CreatePersonAndAcceptInvite(r.Context(), token, username, displayName, email, bcryptHash)
	if err != nil {
		http.Error(w, "couldn't accept this invite: "+err.Error(), http.StatusBadRequest)
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
	// getting-started wizard, carrying the real destination for its
	// "Done"/"skip the rest" step so the invite's own target isn't lost.
	http.Redirect(w, r, "/welcome?next="+url.QueryEscape(dest), http.StatusFound)
}
