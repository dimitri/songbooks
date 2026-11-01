package httpapi

import (
	"context"
	"net/http"

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
	s.render(w, "login", map[string]any{"Error": r.URL.Query().Get("error") != ""})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?error=1", http.StatusFound)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	p, err := s.store.GetPersonByUsername(r.Context(), username)
	if err != nil || p.PasswordHash == "" {
		http.Redirect(w, r, "/login?error=1", http.StatusFound)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(p.PasswordHash), []byte(password)) != nil {
		http.Redirect(w, r, "/login?error=1", http.StatusFound)
		return
	}

	token, err := s.store.CreateSession(r.Context(), p.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
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
