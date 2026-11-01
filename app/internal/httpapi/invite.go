package httpapi

import (
	"net/http"
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
		"Token": inv.Token, "ProjectName": inv.ProjectName, "Role": inv.Role,
		"LoggedInAs": loggedInAs,
	})
}

// handlePostInvite accepts an invite. A logged-in visitor just gets the
// membership added to their existing account; an anonymous one must supply
// a username/display name/password, which creates a brand-new account in
// the same transaction as the membership grant — see
// store.CreatePersonAndAcceptInvite. Either way, the visitor ends up
// logged in (a fresh session cookie) and redirected into the project.
func (s *Server) handlePostInvite(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if p, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
			slug, err := s.store.AcceptInvite(r.Context(), token, p.ID)
			if err != nil {
				http.Error(w, "this invite link is invalid or has expired", http.StatusNotFound)
				return
			}
			http.Redirect(w, r, "/b/"+slug, http.StatusFound)
			return
		}
	}

	username := strings.TrimSpace(r.FormValue("username"))
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	password := r.FormValue("password")
	if username == "" || displayName == "" || password == "" {
		http.Error(w, "username, display name and password are all required", http.StatusBadRequest)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	personID, slug, err := s.store.CreatePersonAndAcceptInvite(r.Context(), token, username, displayName, string(hash))
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
	http.Redirect(w, r, "/b/"+slug, http.StatusFound)
}
