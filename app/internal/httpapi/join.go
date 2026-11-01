package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"app/internal/store"
)

// pendingJoinCookie carries a not-yet-authenticated visitor's chosen
// group-invite slot across a "log in to claim this slot" round trip to
// /login and back. It is a workaround, not a redesign: the magic-link
// login flow (internal/httpapi/auth.go) has no `next`-redirect
// parameter yet, and that file's login-route internals are deliberately
// off limits here (a parallel auth-restoration phase owns them) — see
// handleJoinLoginRedirect and handleIndex's resume step below instead of
// a /login?next=... redirect.
const pendingJoinCookie = "songbooks_pending_join"

// handleJoinPage is the public, no-auth-required group-invite landing
// page — GET /join/{token} — same "must work from a bare WhatsApp link
// with zero cookies" requirement as the existing single-use /invite/
// {token} page. Shows the band/school name, every slot (claimed ones
// are shown but not pickable), an "I'm not on this list" option, and —
// if the visitor is already logged in — skips straight to "pick your
// slot, then confirm."
func (s *Server) handleJoinPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	gi, err := s.store.GetGroupInviteByToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "this invite link is invalid", http.StatusNotFound)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var loggedIn bool
	var loggedInAs string
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if p, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
			loggedIn, loggedInAs = true, p.DisplayName
		}
	}

	s.render(w, "join", map[string]any{
		"Token": token, "Kind": gi.TargetKind, "TargetName": gi.TargetName,
		"Slots": gi.Slots, "Closed": gi.ClosedAt != nil,
		"LoggedIn": loggedIn, "LoggedInAs": loggedInAs,
	})
}

// handleJoinLoginRedirect is the "I already have an account" sub-flow's
// first step: remember which slot the visitor picked in a short-lived
// cookie, then send them to the real login mechanism (magic-link,
// unchanged). handleIndex resumes the claim once they're back and
// authenticated — see pendingJoinCookie's doc comment for why this
// cookie-based resume exists instead of a /login?next= redirect.
func (s *Server) handleJoinLoginRedirect(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	slot := strings.TrimSpace(r.URL.Query().Get("slot"))
	if slot == "" {
		slot = "unlisted"
	}
	http.SetCookie(w, &http.Cookie{
		Name: pendingJoinCookie, Value: token + ":" + slot, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: 15 * 60,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// handlePostJoin accepts a group invite's slot (or an unlisted "someone
// else" join). A logged-in visitor just gets the membership added to
// their existing account on top of whatever they already have; an
// anonymous one must supply username/display name/email, which creates
// a brand-new, passwordless account in the same transaction as the
// claim — same "accepting IS this account's first authentication"
// convention as the single-use /invite/{token} flow (see
// store.CreatePersonAndClaimGroupInviteSlot's doc comment for the note
// on the parallel optional-password auth work).
func (s *Server) handlePostJoin(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	slotParam := strings.TrimSpace(r.FormValue("slot_id"))

	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if p, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
			dest, err := s.claimGroupInviteForPerson(r.Context(), token, slotParam, p.ID, p.DisplayName)
			if err != nil {
				writeJoinError(w, err)
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
		httpError(w, http.StatusBadRequest, "username, display name and email are all required")
		return
	}

	var personID int64
	var dest string
	var err error
	if slotParam == "" || slotParam == "unlisted" {
		personID, dest, err = s.store.CreatePersonAndClaimGroupInviteUnlisted(r.Context(), token, username, displayName, email)
	} else {
		slotID, perr := strconv.ParseInt(slotParam, 10, 64)
		if perr != nil {
			httpError(w, http.StatusBadRequest, "invalid slot")
			return
		}
		personID, dest, err = s.store.CreatePersonAndClaimGroupInviteSlot(r.Context(), token, slotID, username, displayName, email)
	}
	if err != nil {
		writeJoinError(w, err)
		return
	}

	sessToken, err := s.store.CreateSession(r.Context(), personID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sessToken, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: 30 * 24 * 60 * 60,
	})
	http.Redirect(w, r, dest, http.StatusFound)
}

// claimGroupInviteForPerson is the shared "claim slot or unlisted join"
// dispatch used both by handlePostJoin (an already-logged-in visitor)
// and handleIndex's pending-claim resume step.
func (s *Server) claimGroupInviteForPerson(ctx context.Context, token, slotParam string, personID int64, displayName string) (string, error) {
	if slotParam == "" || slotParam == "unlisted" {
		return s.store.ClaimGroupInviteUnlisted(ctx, token, personID, displayName)
	}
	slotID, err := strconv.ParseInt(slotParam, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid slot")
	}
	return s.store.ClaimGroupInviteSlot(ctx, token, slotID, personID)
}

func writeJoinError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, store.ErrForbidden):
		status = http.StatusGone // invite explicitly closed
	case errors.Is(err, store.ErrConflict):
		status = http.StatusConflict // slot already claimed
	}
	http.Error(w, err.Error(), status)
}
