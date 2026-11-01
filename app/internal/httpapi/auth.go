package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"

	"app/internal/store"
)

const sessionCookie = "songbooks_session"

type ctxKey int

const personCtxKey ctxKey = 0

func personFromContext(r *http.Request) *store.Person {
	p, _ := r.Context().Value(personCtxKey).(*store.Person)
	return p
}

// actorID returns the logged-in person's id for revision-actor
// bookkeeping (store.SaveSong/SaveRaw/RedoSong/...), or 0 ("unknown
// actor") if no person is in context.
func actorID(r *http.Request) int64 {
	if p := personFromContext(r); p != nil {
		return p.ID
	}
	return 0
}

// requireAuth loads the session cookie (if any) and stores the person in
// request context for next. An unauthenticated page request redirects to
// /login; an unauthenticated /api/* request gets a 401 JSON body instead of
// a redirect, since it has no browser chrome to redirect.
//
// This guards "is anyone logged in" only. Jam mode (Phase 4, not built
// yet) lets an unlogged nickname-only participant read one event's project
// songs — that will need its own, separate, narrower guard alongside this
// one, not a change to this one; don't fold jam access into this check.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Dev-only escape hatch (see Server.devAutoLogin's doc comment):
		// when set, skip the session-cookie check entirely and act as
		// already logged in as that person. s.devAutoLogin is nil unless
		// --dev-auto-login/SONGBOOKS_DEV_AUTO_LOGIN was explicitly passed,
		// so this branch is dead code — never taken — on every deployment
		// that didn't deliberately opt in, including production.
		if s.devAutoLogin != nil {
			ctx := context.WithValue(r.Context(), personCtxKey, s.devAutoLogin)
			next(w, r.WithContext(ctx))
			return
		}
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			s.denyUnauthenticated(w, r)
			return
		}
		p, err := s.store.GetSession(r.Context(), cookie.Value)
		if err != nil {
			s.denyUnauthenticated(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), personCtxKey, p)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) denyUnauthenticated(w http.ResponseWriter, r *http.Request) {
	if isAPIRequest(r) {
		httpError(w, http.StatusUnauthorized, "login required")
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

func isAPIRequest(r *http.Request) bool {
	return len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/"
}

var roleRank = map[string]int{"viewer": 1, "editor": 2, "owner": 3}

// requireRole wraps next with requireAuth, then additionally checks the
// logged-in person's role on the {book} path param meets at least minRole.
// Not a project member at all, or below minRole, both get the same 403 —
// a non-member shouldn't learn a project exists from the status code.
func (s *Server) requireRole(minRole string, next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		book := r.PathValue("book")
		p := personFromContext(r)
		role, ok, err := s.store.GetRole(r.Context(), p.ID, book)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok || roleRank[role] < roleRank[minRole] {
			if isAPIRequest(r) {
				httpError(w, http.StatusForbidden, "insufficient project role")
			} else {
				http.Error(w, "forbidden", http.StatusForbidden)
			}
			return
		}
		next(w, r)
	})
}

// requireAdmin wraps next with requireAuth, then additionally checks the
// logged-in person's site-wide is_admin flag — deliberately a separate
// middleware from requireRole, since admin is a property of the person
// (every project, every person), not a role scoped to one project's
// {book} path param the way requireRole's check is.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		p := personFromContext(r)
		if p == nil || !p.IsAdmin {
			if isAPIRequest(r) {
				httpError(w, http.StatusForbidden, "admin only")
			} else {
				http.Error(w, "forbidden", http.StatusForbidden)
			}
			return
		}
		next(w, r)
	})
}

// requireJamOrganizer gates a {book}/{code}-scoped jam-management endpoint
// (song reorder, "now calling") on "is a designated organizer for this
// jam event OR has editor/owner on the parent project" — the Phase 5
// rule. Distinct from requireRole (project-scoped only) and from
// requireAdmin (site-wide only): a jam organizer is scoped to one jam
// event and need not have project-wide editor rights at all.
func (s *Server) requireJamOrganizer(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		book := r.PathValue("book")
		code := r.PathValue("code")
		p := personFromContext(r)
		ev, err := s.store.GetJamEvent(r.Context(), code)
		if err != nil {
			httpError(w, http.StatusNotFound, "this jam link is invalid or has expired")
			return
		}
		isOrganizer, err := s.store.IsJamOrganizer(r.Context(), ev.ID, p.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if isOrganizer {
			next(w, r)
			return
		}
		role, ok, err := s.store.GetRole(r.Context(), p.ID, book)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ok && roleRank[role] >= roleRank["editor"] {
			next(w, r)
			return
		}
		httpError(w, http.StatusForbidden, "organizer status (or editor/owner on this project) required")
	})
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if _, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}
	s.render(w, "login", map[string]any{
		"Error":              r.URL.Query().Get("error") != "",
		"InvalidCredentials": r.URL.Query().Get("badlogin") != "",
	})
}

// handleLogin is POST /login, and branches cleanly on which of the two,
// equally first-class login forms the visitor submitted (see login.html):
// a password ("mode=password", handled by handlePasswordLogin) or the
// magic-link "email me a link" request (handled inline below, unchanged
// from before password login was restored). Both forms post to this same
// route; the hidden "mode" field is the only thing that tells them apart.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.render(w, "login_sent", nil)
		return
	}

	if r.FormValue("mode") == "password" {
		s.handlePasswordLogin(w, r)
		return
	}

	// Magic-link login's request step: look up the email, and if it
	// belongs to an account, email ("send" — see internal/mailer) a
	// single-use login link that expires in 15 minutes. The response is
	// the exact same "check your email" page whether or not the email
	// matched a real account — not leaking account existence is a real
	// security property here, not polish, so every path through this
	// handler (lookup miss, store error, mailer error, success) renders
	// the same template with no per-case data baked into it.
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))

	if email != "" {
		if p, err := s.store.GetPersonByEmail(r.Context(), email); err == nil {
			if token, err := s.store.CreateLoginToken(r.Context(), p.ID); err == nil {
				link := s.loginLinkURL(r, token)
				subject := "Your Songbooks login link"
				body := fmt.Sprintf("Hi %s,\n\nClick this link to log in to Songbooks (expires in 15 minutes):\n\n%s\n\nIf you didn't request this, you can ignore this email.", p.DisplayName, link)
				if err := s.mailer.Send(r.Context(), p.Email, subject, body); err != nil {
					log.Printf("sending login email: %v", err)
				}
			} else {
				log.Printf("creating login token: %v", err)
			}
		}
		// A lookup miss (err != nil) falls straight through to the same
		// response below — deliberately no branch on "found or not".
	}

	s.render(w, "login_sent", nil)
}

// dummyPasswordHash is a bcrypt hash of a random, never-revealed value,
// computed once on first use. handlePasswordLogin compares against it
// whenever the submitted identifier doesn't resolve to a real account (or
// resolves to one with no password set) — so a lookup miss and a correct-
// account-wrong-password both run one bcrypt comparison before failing,
// rather than the former returning instantly. This doesn't make the two
// cases indistinguishable in wall-clock time (bcrypt against a fixed hash
// vs. a real one isn't guaranteed identical cost), but it closes the
// obvious "lookup miss short-circuits before any hashing" gap; the
// response body/status in both cases is already byte-identical, which is
// the property that actually matters here.
var (
	dummyPasswordHashOnce sync.Once
	dummyPasswordHash     string
)

func getDummyPasswordHash() string {
	dummyPasswordHashOnce.Do(func() {
		h, err := bcrypt.GenerateFromPassword([]byte("songbooks-dummy-hash-no-such-password"), bcrypt.DefaultCost)
		if err != nil {
			// GenerateFromPassword only fails on a bad cost constant, which
			// bcrypt.DefaultCost never is — this is unreachable in practice.
			panic("hashing dummy password: " + err.Error())
		}
		dummyPasswordHash = string(h)
	})
	return dummyPasswordHash
}

// handlePasswordLogin is POST /login's password branch (mode=password):
// verify bcrypt against people.password_hash for the account identified
// by "identifier" (username or email, either is accepted), and log in
// directly on success. Any failure — no such account, account has no
// password set yet (PasswordHash == ""), or wrong password — redirects
// back to /login with the exact same generic "invalid credentials" error,
// deliberately not distinguished further: this path has the same
// non-enumeration requirement the magic-link path already honors.
func (s *Server) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	identifier := strings.TrimSpace(r.FormValue("identifier"))
	password := r.FormValue("password")

	var p *store.Person
	if identifier != "" {
		var err error
		if strings.Contains(identifier, "@") {
			p, err = s.store.GetPersonByEmail(r.Context(), strings.ToLower(identifier))
		} else {
			p, err = s.store.GetPersonByUsername(r.Context(), identifier)
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			log.Printf("password login lookup for %q: %v", identifier, err)
		}
	}

	hash := getDummyPasswordHash()
	if p != nil && p.PasswordHash != "" {
		hash = p.PasswordHash
	}
	// Always run the comparison, even against the dummy hash — see
	// dummyPasswordHash's doc comment above.
	credsOK := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	if p == nil || p.PasswordHash == "" || !credsOK {
		http.Redirect(w, r, "/login?badlogin=1", http.StatusFound)
		return
	}

	sessToken, err := s.store.CreateSession(r.Context(), p.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sessToken, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: 30 * 24 * 60 * 60,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// loginLinkURL builds the magic-link URL embedded in the login email,
// from the incoming request's own scheme/host — this app has no
// separate "public base URL" config yet.
func (s *Server) loginLinkURL(r *http.Request, token string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/login/confirm?token=%s", scheme, r.Host, token)
}

// handleLoginConfirm validates a magic-link token (unused, unexpired),
// starts a normal session, and redirects to the dashboard. An
// invalid/expired/already-used token gets a clear error page, not a
// crash or a generic 500.
func (s *Server) handleLoginConfirm(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		s.render(w, "login_confirm_error", nil)
		return
	}
	p, err := s.store.ConsumeLoginToken(r.Context(), token)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			log.Printf("consuming login token: %v", err)
		}
		s.render(w, "login_confirm_error", nil)
		return
	}

	sessToken, err := s.store.CreateSession(r.Context(), p.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sessToken, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: 30 * 24 * 60 * 60,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.DeleteSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}
