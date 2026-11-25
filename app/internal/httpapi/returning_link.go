package httpapi

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"app/internal/store"
)

// A personal link keeps opening for its person until it expires
// (store.GetReturningLink; decided 2026-11-25: people use several
// devices, and open the same link again on each). Its first use makes
// the account (invite.go); every later use is that person coming back:
//
//   - signed out: "Welcome back, Guillaume", their password (the
//     account's email is known: shown masked, never in full) or their
//     passkey, then the band or community the link was for. The link
//     never signs anyone in by itself: it is shareable;
//   - signed in as them: straight to that band or community;
//   - signed in as someone else: "This link is Guillaume's", with Log
//     out and Go home. Never a merge of one account into another.
//
// Expired or revoked, it gets the dead link's page (dead_link.go).

// linkSignInTries bounds wrong passwords through personal links, per
// account: a link names the account, so whoever has it may try that
// account's password.
var linkSignInTries = newTryLimiter(10, 15*time.Minute)

// handleReturningLink is GET /invite/{token} for a link for coming back.
func (s *Server) handleReturningLink(w http.ResponseWriter, r *http.Request, l *store.ReturningLink) {
	w.Header().Set("Cache-Control", "no-store")
	if p := s.sessionPerson(r); p != nil {
		if p.ID == l.PersonID {
			http.Redirect(w, r, s.returningLinkDest(r.Context(), l), http.StatusSeeOther)
			return
		}
		s.renderReturningLink(w, http.StatusOK, l, p.DisplayName, "")
		return
	}
	s.renderReturningLink(w, http.StatusOK, l, "", "")
}

// handlePostReturningLink is POST /invite/{token} for a link for coming
// back: the account's password, and nothing else. Whatever else the
// form carries (an email, a password typed twice: the first use's form)
// is ignored: the link can't change the account's email or password,
// nor make another account.
func (s *Server) handlePostReturningLink(w http.ResponseWriter, r *http.Request, l *store.ReturningLink) {
	w.Header().Set("Cache-Control", "no-store")
	if p := s.sessionPerson(r); p != nil {
		if p.ID == l.PersonID {
			http.Redirect(w, r, s.returningLinkDest(r.Context(), l), http.StatusSeeOther)
			return
		}
		s.renderReturningLink(w, http.StatusOK, l, p.DisplayName, "")
		return
	}
	key := fmt.Sprint(l.PersonID)
	if !linkSignInTries.allow(key) {
		log.Printf("personal link: too many wrong passwords for person %d", l.PersonID)
		s.renderReturningLink(w, http.StatusTooManyRequests, l, "", "too-many")
		return
	}
	password := r.FormValue("password")
	// One bcrypt comparison whatever the account (no password set: the
	// dummy hash), and the login page's one answer for every failure.
	p, err := s.store.GetPersonByID(r.Context(), l.PersonID)
	if err != nil {
		log.Printf("personal link: person %d: %v", l.PersonID, err)
		p = nil
	}
	hash := getDummyPasswordHash()
	if p != nil && p.PasswordHash != "" {
		hash = p.PasswordHash
	}
	ok := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	if p == nil || p.PasswordHash == "" || !ok {
		linkSignInTries.fail(key)
		s.renderReturningLink(w, http.StatusOK, l, "", "bad-password")
		return
	}
	linkSignInTries.reset(key)
	if err := s.startSession(w, r, p.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.RecordLinkSignIn(r.Context(), l.Token); err != nil {
		log.Printf("personal link: counting a sign-in: %v", err)
	}
	http.Redirect(w, r, s.returningLinkDest(r.Context(), l), http.StatusSeeOther)
}

// renderReturningLink draws the link's page: signed out (signedInAs
// ""), "Welcome back" and the password or passkey, with problem
// ("bad-password", "too-many") said above it; signed in as someone
// else, whose link it is.
func (s *Server) renderReturningLink(w http.ResponseWriter, status int, l *store.ReturningLink, signedInAs, problem string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, "link_return", map[string]any{
		"Token": l.Token, "Name": l.DisplayName, "MaskedEmail": maskEmail(l.Email),
		"Until": l.ExpiresAt, "TargetName": l.TargetName,
		"SignedInAs": signedInAs, "Problem": problem,
		"LoginEmails": s.loginEmails(),
	})
}

// returningLinkDest is where the link takes its person: its band or
// community when they're still in it, else home.
func (s *Server) returningLinkDest(ctx context.Context, l *store.ReturningLink) string {
	switch l.TargetKind {
	case "band":
		if ok, err := s.store.IsCurrentBandMember(ctx, l.TargetID, l.PersonID); err == nil && ok {
			return fmt.Sprintf("/bands/%d", l.TargetID)
		}
	case "community":
		if ok, err := s.store.IsCurrentCommunityMember(ctx, l.TargetID, l.PersonID); err == nil && ok {
			return fmt.Sprintf("/communities/%d", l.TargetID)
		}
	}
	return "/home"
}

// startSession signs personID in on this browser: the session cookie,
// as every way of logging in sets it.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, personID int64) error {
	tok, err := s.store.CreateSession(r.Context(), personID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: 30 * 24 * 60 * 60,
	})
	return nil
}

// maskEmail shows an email without giving it away: its first letter,
// then dots, and the domain when it's a common mail provider's
// ("g•••@gmail.com"), else that domain's first letter and its last part
// ("g•••@d•••.fr"). "" stays "".
func maskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" {
		return ""
	}
	const dots = "•••"
	first, _ := utf8.DecodeRuneInString(local)
	masked := string(first) + dots + "@"
	if commonMailDomains[strings.ToLower(domain)] {
		return masked + domain
	}
	d, _ := utf8.DecodeRuneInString(domain)
	masked += string(d) + dots
	if i := strings.LastIndexByte(domain, '.'); i > 0 && i < len(domain)-1 {
		masked += domain[i:]
	}
	return masked
}

// commonMailDomains are mail providers shared by many people: naming
// one says nothing about who the account is.
var commonMailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "outlook.fr": true,
	"hotmail.com": true, "hotmail.fr": true, "live.com": true, "live.fr": true, "msn.com": true,
	"yahoo.com": true, "yahoo.fr": true, "icloud.com": true, "me.com": true, "mac.com": true,
	"proton.me": true, "protonmail.com": true, "gmx.com": true, "gmx.fr": true, "gmx.de": true,
	"free.fr": true, "orange.fr": true, "wanadoo.fr": true, "sfr.fr": true, "laposte.net": true,
	"bbox.fr": true, "neuf.fr": true, "aol.com": true, "fastmail.com": true,
}

// tryLimiter counts failures per key over a window: after max of them,
// the key is refused until the window that started with its first
// failure ends. In memory: a restart forgets, which is fine for slowing
// down guessing.
type tryLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	tries  map[string]tryCount
}

type tryCount struct {
	n     int
	since time.Time
}

func newTryLimiter(max int, window time.Duration) *tryLimiter {
	return &tryLimiter{max: max, window: window, tries: map[string]tryCount{}}
}

// allow reports whether key may try again.
func (l *tryLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.tries[key]
	if !ok {
		return true
	}
	if time.Since(c.since) > l.window {
		delete(l.tries, key)
		return true
	}
	return c.n < l.max
}

// fail counts a failure for key.
func (l *tryLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// Forget the windows that ended, so the map doesn't grow.
	for k, c := range l.tries {
		if now.Sub(c.since) > l.window {
			delete(l.tries, k)
		}
	}
	c, ok := l.tries[key]
	if !ok {
		c = tryCount{since: now}
	}
	c.n++
	l.tries[key] = c
}

// reset forgets key's failures (it got in).
func (l *tryLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.tries, key)
}
