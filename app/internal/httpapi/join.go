package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"app/internal/store"
)

// pendingJoinCookie carries a not-yet-authenticated visitor's join
// across a "log in to join" round trip to /login and back: every way
// in by a link (a group invite's slot, an invite or personal link, an
// event's registration) offers it to someone whose email has an account
// already. The login page (password or emailed link) lands on /home,
// where handleIndex finishes the join (resumePendingJoin). Its value:
//
//	<group invite token>:<slot id or "unlisted">   a /join/ link
//	invite:<token>                                 an /invite/ link
//	event:<id>                                     an event to register for
const pendingJoinCookie = "songbooks_pending_join"

// The pendingJoinCookie prefixes besides a group invite's token.
const (
	pendingInvite = "invite:"
	pendingEvent  = "event:"
)

// loginThenResume remembers what to join (a pendingJoinCookie value,
// for 15 minutes: a login link lasts as long) and sends the visitor to
// log in.
func (s *Server) loginThenResume(w http.ResponseWriter, r *http.Request, value string) {
	http.SetCookie(w, &http.Cookie{
		Name: pendingJoinCookie, Value: value, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: 15 * 60,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// resumePendingJoin finishes the join a pendingJoinCookie value names,
// for person, now logged in, and says where to go: the band, community
// or event; false when it can't be done any more (a link used, closed
// or expired meanwhile), and the visitor just gets their home page.
func (s *Server) resumePendingJoin(ctx context.Context, value string, person *store.Person) (string, bool) {
	switch {
	case strings.HasPrefix(value, pendingInvite):
		token := strings.TrimPrefix(value, pendingInvite)
		inv, err := s.store.GetInvite(ctx, token)
		if err != nil {
			return "", false
		}
		if inv.Kind() == "personal" {
			pv, err := s.store.GetPersonalInvite(ctx, token)
			if err != nil || s.store.ClaimPersonalInviteAs(ctx, token, person.ID) != nil {
				return "", false
			}
			return personalInviteDest(pv), true
		}
		dest, err := s.store.AcceptInvite(ctx, token, person.ID, person.DisplayName)
		return dest, err == nil
	case strings.HasPrefix(value, pendingEvent):
		id, err := strconv.ParseInt(strings.TrimPrefix(value, pendingEvent), 10, 64)
		if err != nil {
			return "", false
		}
		// Back to the event, to register as themselves: what they'll
		// play, as whom (a band of theirs) is theirs to say there.
		return "/events/" + strconv.FormatInt(id, 10), true
	default:
		token, slotParam, ok := strings.Cut(value, ":")
		if !ok {
			return "", false
		}
		dest, err := s.claimGroupInviteForPerson(ctx, token, slotParam, person.ID, person.DisplayName)
		return dest, err == nil
	}
}

// handleJoinPage is the public, no-auth-required group-invite landing
// page — GET /join/{token} — same "must work from a bare WhatsApp link
// with zero cookies" requirement as the existing single-use /invite/
// {token} page. Shows the band/community name, every slot (claimed ones
// are shown but not pickable), an "I'm not on this list" option, and —
// if the visitor is already logged in — skips straight to "pick your
// slot, then confirm." An unknown or closed link gets
// handleDeadJoinLink's page.
func (s *Server) handleJoinPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	gi, err := s.store.GetGroupInviteByToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		s.handleDeadJoinLink(w, r, nil)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if gi.ClosedAt != nil {
		s.handleDeadJoinLink(w, r, gi)
		return
	}

	var loggedIn bool
	var loggedInAs string
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if p, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
			loggedIn, loggedInAs = true, p.DisplayName
		}
	}

	// An open link (a band or community link, handleCreateBandLink /
	// handleCreateCommunityLink) has no name left to pick: the page
	// skips "Who are you?" and goes straight to joining. Its slots are
	// only the people who already joined through it, which a public
	// page has no business listing.
	open := true
	for _, sl := range gi.Slots {
		if sl.ClaimedAt == nil {
			open = false
		}
	}

	s.render(w, "join", map[string]any{
		"Token": token, "Kind": gi.TargetKind, "TargetName": gi.TargetName,
		"Slots": gi.Slots, "Open": open,
		"LoggedIn": loggedIn, "LoggedInAs": loggedInAs,
		"LoginEmails": s.loginEmails(),
	})
}

// handleJoinLoginRedirect is the "I already have an account" sub-flow's
// first step: remember which slot the visitor picked (pendingJoinCookie)
// and send them to log in; handleIndex finishes the claim once they're
// back (resumePendingJoin).
func (s *Server) handleJoinLoginRedirect(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	gi, err := s.store.GetGroupInviteByToken(r.Context(), token)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if gi == nil || gi.ClosedAt != nil {
		s.handleDeadJoinLink(w, r, gi)
		return
	}
	slot := strings.TrimSpace(r.URL.Query().Get("slot"))
	if slot == "" {
		slot = "unlisted"
	}
	s.loginThenResume(w, r, token+":"+slot)
}

// handlePostJoin accepts a group invite's slot (or an unlisted "someone
// else" join). A logged-in visitor just gets the membership added to
// their existing account on top of whatever they already have; an
// anonymous one must supply a display name, an email and a password,
// which creates a brand-new account in the same transaction as the
// claim. The password is required for the same reason as on the
// single-use /invite/{token} flow: no login emails are sent, so the
// passkey offered next can't be the only way back in.
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

	displayName := strings.TrimSpace(r.FormValue("display_name"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	if displayName == "" || email == "" {
		httpError(w, http.StatusBadRequest, "display name and email are both required")
		return
	}
	if at := strings.IndexByte(email, '@'); at <= 0 || at == len(email)-1 {
		httpError(w, http.StatusBadRequest, "enter a valid email address")
		return
	}
	password := r.FormValue("password")
	if msg := newPasswordProblem(password, r.FormValue("confirm")); msg != "" {
		httpError(w, http.StatusBadRequest, msg)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "couldn't create the account, try again")
		return
	}

	var personID int64
	var dest string
	if slotParam == "" || slotParam == "unlisted" {
		personID, dest, err = s.store.CreatePersonAndClaimGroupInviteUnlisted(r.Context(), token, displayName, email, string(hash))
	} else {
		slotID, perr := strconv.ParseInt(slotParam, 10, 64)
		if perr != nil {
			httpError(w, http.StatusBadRequest, "invalid slot")
			return
		}
		personID, dest, err = s.store.CreatePersonAndClaimGroupInviteSlot(r.Context(), token, slotID, displayName, email, string(hash))
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
	// personID was just created above (unlike the already-logged-in
	// branch earlier in this function) — offer the getting-started
	// wizard, carrying the real destination for its "Done" step.
	http.Redirect(w, r, "/welcome?next="+url.QueryEscape(dest), http.StatusFound)
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
	case errors.Is(err, store.ErrEmailTaken):
		status = http.StatusConflict // "email already in use…": the form says so under the field
	}
	http.Error(w, err.Error(), status)
}
