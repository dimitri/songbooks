// Package httpapi: passkey (WebAuthn) login — a THIRD first-class login
// option alongside password (auth.go's handlePasswordLogin) and magic-link
// (auth.go's handleLogin), additive only. Registration (adding a passkey to
// an already-logged-in account) and login (discoverable/"passwordless",
// see handleWebAuthnLoginBegin's doc comment for why that UX was chosen
// over identifier-then-passkey) each run a two-step begin/finish ceremony
// against github.com/go-webauthn/webauthn.
//
// Relying Party configuration (RPID/origin) is Server.webauthnRPID — see
// webauthnForRequest's doc comment and cmd/songbooks/main.go's
// --webauthn-rp-id flag for why this must be pinned explicitly in
// production and must NOT be left to derive from the request Host there.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"app/internal/store"
)

// webauthnLoginCookie carries a random, short-lived flow id that ties a
// discoverable-login /begin call to its matching /finish call, for the one
// case (login) where there's no session cookie yet to key off of —
// registration instead keys off the already-logged-in person's id (see
// webauthnRegSessions). It is NOT the session cookie (sessionCookie in
// auth.go) and grants no access by itself: it only resolves to a pending
// challenge, which only proves itself by producing a valid signed
// assertion for that challenge.
const webauthnLoginCookie = "songbooks_webauthn_login"

// webauthnCeremonyTTL bounds how long a begun-but-unfinished ceremony's
// SessionData is kept around before being treated as abandoned — generous
// relative to the authenticator UI's own ~60s timeout, just enough to stop
// the in-memory maps from growing unboundedly across abandoned ceremonies.
const webauthnCeremonyTTL = 5 * time.Minute

type pendingWebAuthnSession struct {
	session *webauthn.SessionData
	expires time.Time
}

// webauthnState holds every in-flight (begun, not yet finished) WebAuthn
// ceremony's SessionData — deliberately in-memory, not in Postgres: a
// challenge is only ever useful for the single follow-up request a few
// seconds later, never needs to survive a restart, and (per the library's
// own SessionData doc comment) must be opaque to and unmodifiable by the
// client, which an in-process map trivially satisfies.
type webauthnState struct {
	mu  sync.Mutex
	reg map[int64]pendingWebAuthnSession  // keyed by person id (registration: already logged in)
	log map[string]pendingWebAuthnSession // keyed by the webauthnLoginCookie flow id (login: not logged in yet)
}

func newWebAuthnState() *webauthnState {
	return &webauthnState{reg: map[int64]pendingWebAuthnSession{}, log: map[string]pendingWebAuthnSession{}}
}

func (st *webauthnState) putReg(personID int64, s *webauthn.SessionData) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.reg[personID] = pendingWebAuthnSession{session: s, expires: time.Now().Add(webauthnCeremonyTTL)}
}

// takeReg returns and removes the pending registration SessionData for
// personID — single-use, same "consumed by the one matching finish call"
// shape as the login flow id below.
func (st *webauthnState) takeReg(personID int64) (*webauthn.SessionData, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	p, ok := st.reg[personID]
	delete(st.reg, personID)
	if !ok || time.Now().After(p.expires) {
		return nil, false
	}
	return p.session, true
}

func (st *webauthnState) putLogin(flowID string, s *webauthn.SessionData) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.log[flowID] = pendingWebAuthnSession{session: s, expires: time.Now().Add(webauthnCeremonyTTL)}
}

func (st *webauthnState) takeLogin(flowID string) (*webauthn.SessionData, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	p, ok := st.log[flowID]
	delete(st.log, flowID)
	if !ok || time.Now().After(p.expires) {
		return nil, false
	}
	return p.session, true
}

// webauthnForRequest returns the *webauthn.WebAuthn configured for this
// request's Relying Party. If Server.webauthnRPID was set explicitly
// (--webauthn-rp-id / SONGBOOKS_WEBAUTHN_RP_ID — see main.go), that fixed
// RPID/origin is used for every request, which is what production MUST do:
// a passkey is bound to the RPID it was registered under, and
// songbooks.tapoueh.org's passkeys need that exact RPID every time,
// regardless of what Host header a request happens to carry.
//
// If unset (the default — local dev convenience only), the RPID/origin are
// derived from THIS request's own Host/scheme instead, recomputed per
// request. That's deliberately not how production should run: it means
// "whatever host you hit it through becomes the RP", which is fine for
// bouncing between localhost:8080 and a LAN IP while developing, but would
// let a passkey silently bind to whatever hostname happened to be used
// that day on a real deployment — see deploy/README.md.
//
// A passkey registered under one RPID/origin will NOT work under another —
// this is WebAuthn's origin-binding working as designed, not a bug to route
// around. A passkey made against localhost in dev and one made against
// songbooks.tapoueh.org in production are simply two unrelated
// credentials, by construction.
func (s *Server) webauthnForRequest(r *http.Request) (*webauthn.WebAuthn, error) {
	rpID := s.webauthnRPID
	var origin string
	if rpID != "" {
		origin = "https://" + rpID
	} else {
		rpID = requestHostname(r)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		origin = scheme + "://" + r.Host
	}
	return webauthn.New(&webauthn.Config{
		RPDisplayName: "Songbooks",
		RPID:          rpID,
		RPOrigins:     []string{origin},
	})
}

// requestHostname strips any :port from r.Host — WebAuthn's RPID must be a
// bare hostname (a "registrable domain suffix" of the origin), never
// host:port.
func requestHostname(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.Host); err == nil {
		return host
	}
	return r.Host
}

// webauthnUser adapts a store.Person plus their already-loaded passkeys to
// the webauthn.User interface the library's Begin*/Finish* calls need.
// WebAuthnID is the person's id, big-endian encoded — stable, never reused
// (people are never deleted and re-created at the same id in this app),
// and never displayed, matching the interface's own requirement that
// authentication decisions be made on this id, not on name/display name.
type webauthnUser struct {
	person *store.Person
	creds  []store.WebAuthnCredential
}

func personIDBytes(id int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(id))
	return b
}

func personIDFromBytes(b []byte) (int64, bool) {
	if len(b) != 8 {
		return 0, false
	}
	return int64(binary.BigEndian.Uint64(b)), true
}

func (u webauthnUser) WebAuthnID() []byte   { return personIDBytes(u.person.ID) }
func (u webauthnUser) WebAuthnName() string { return u.person.Username }
func (u webauthnUser) WebAuthnDisplayName() string {
	if u.person.DisplayName != "" {
		return u.person.DisplayName
	}
	return u.person.Username
}

func (u webauthnUser) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, len(u.creds))
	for i, c := range u.creds {
		out[i] = toLibCredential(c)
	}
	return out
}

func toLibCredential(c store.WebAuthnCredential) webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, len(c.Transports))
	for i, t := range c.Transports {
		transports[i] = protocol.AuthenticatorTransport(t)
	}
	return webauthn.Credential{
		ID:              c.CredentialID,
		PublicKey:       c.PublicKey,
		AttestationType: c.AttestationType,
		Transport:       transports,
		Flags: webauthn.CredentialFlags{
			UserPresent:    c.FlagUserPresent,
			UserVerified:   c.FlagUserVerified,
			BackupEligible: c.FlagBackupEligible,
			BackupState:    c.FlagBackupState,
		},
		Authenticator: webauthn.Authenticator{SignCount: c.SignCount},
	}
}

// randomFlowID returns a random URL-safe token for webauthnLoginCookie.
func randomFlowID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ---- Registration: POST /api/webauthn/register/begin, /finish ----
// Both requireAuth-gated (see router.go) — a passkey is added to an
// EXISTING account, never a way to create one (account creation stays
// invite-only; see store.CreatePersonAndAcceptInvite).

// handleWebAuthnRegisterBegin starts the registration ceremony: fetches
// the logged-in person's existing passkeys (so the new ceremony can
// exclude them via CredentialExcludeList — stops a device re-registering
// a passkey it already holds), asks for a discoverable credential
// (ResidentKey required) so the login side can later use the no-identifier
// "just pick one" UX, and stashes the resulting challenge/SessionData
// keyed by person id until /finish.
func (s *Server) handleWebAuthnRegisterBegin(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	creds, err := s.store.ListWebAuthnCredentials(r.Context(), p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	wa, err := s.webauthnForRequest(r)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	user := webauthnUser{person: p, creds: creds}
	exclude := make([]protocol.CredentialDescriptor, len(creds))
	for i, c := range creds {
		lc := toLibCredential(c)
		exclude[i] = lc.Descriptor()
	}
	creation, session, err := wa.BeginRegistration(user,
		webauthn.WithExclusions(exclude),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationPreferred,
		}),
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
	)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "starting passkey registration: "+err.Error())
		return
	}
	s.webauthn.putReg(p.ID, session)
	writeJSON(w, http.StatusOK, creation)
}

// handleWebAuthnRegisterFinish's request body is the standard
// PublicKeyCredential JSON produced by the browser's credential.toJSON()
// (matching protocol.CredentialCreationResponse's own JSON shape) — passed
// straight through to FinishRegistration below, which reads r.Body itself.
// Nothing is asked of the person once the platform's own sheet closes: the
// new passkey is named from its AAGUID (the provider: "Apple Passwords",
// "Google Password Manager"…) or, when that's unknown or zeroed, from this
// request's User-Agent — the current passkey-management guidance
// (web.dev, Chrome, Android), since most people skip a naming prompt and a
// typed "iPhone" goes stale once the passkey syncs elsewhere.
func (s *Server) handleWebAuthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	session, ok := s.webauthn.takeReg(p.ID)
	if !ok {
		httpError(w, http.StatusBadRequest, "no passkey registration in progress (it may have expired) — try again")
		return
	}
	wa, err := s.webauthnForRequest(r)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	creds, err := s.store.ListWebAuthnCredentials(r.Context(), p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	user := webauthnUser{person: p, creds: creds}

	credential, err := wa.FinishRegistration(user, *session, r)
	if err != nil {
		httpError(w, http.StatusBadRequest, "passkey registration failed: "+err.Error())
		return
	}

	transports := make([]string, len(credential.Transport))
	for i, t := range credential.Transport {
		transports[i] = string(t)
	}
	if _, err := s.store.AddWebAuthnCredential(r.Context(), p.ID,
		credential.ID, credential.PublicKey, credential.AttestationType, transports, credential.Authenticator.SignCount,
		credential.Flags.BackupEligible, credential.Flags.BackupState, credential.Flags.UserPresent, credential.Flags.UserVerified,
		aaguidString(credential.Authenticator.AAGUID), passkeyDeviceLabel(r.UserAgent())); err != nil {
		httpError(w, http.StatusInternalServerError, "saving new passkey: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleSetWebAuthnNickname is POST /profile/passkeys/{id}/nickname — a
// plain HTML form post from the profile page's management list (not a
// JSON API endpoint, consistent with every other /profile/* mutation in
// this file's sibling profile.go). Renaming is optional, tucked behind a
// "Rename" disclosure on each row; an empty name goes back to the
// automatic one.
func (s *Server) handleSetWebAuthnNickname(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid passkey id")
		return
	}
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	nickname := strings.TrimSpace(r.FormValue("nickname"))
	if utf8.RuneCountInString(nickname) > passkeyNicknameMax {
		nickname = string([]rune(nickname)[:passkeyNicknameMax])
	}
	if err := s.store.SetWebAuthnCredentialNickname(r.Context(), p.ID, id, nickname); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "no such passkey")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/profile", http.StatusFound)
}

// handleRemoveWebAuthnCredential is POST /profile/passkeys/{id}/remove —
// the profile page's "remove" button next to each registered passkey.
func (s *Server) handleRemoveWebAuthnCredential(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid passkey id")
		return
	}
	if err := s.store.RemoveWebAuthnCredential(r.Context(), p.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "no such passkey")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/profile", http.StatusFound)
}

// ---- Login: POST /api/webauthn/login/begin, /finish ----
// Unauthenticated routes (see router.go, same as /login itself).
//
// Discoverable-credential ("passwordless") UX: BeginDiscoverableLogin asks
// for an assertion with NO allowCredentials list at all, so the browser
// shows its own "pick a passkey for this site" account picker with no
// identifier typed first, and FinishPasskeyLogin resolves which person
// that is FROM the assertion's userHandle (see webauthnDiscoverableHandler
// below) rather than from anything the visitor typed. This is the UX this
// phase chose over identifier-then-passkey (look up by email/username
// first, then call BeginLogin with that one person's own
// allowedCredentials): go-webauthn supports both equally directly
// (BeginLogin/FinishLogin vs. BeginDiscoverableLogin/FinishPasskeyLogin are
// symmetric, same amount of code either way), so there was no "harder to
// wire up" tradeoff to fall back from — discoverable won on UX merits
// alone, per the task's own stated preference.
func (s *Server) handleWebAuthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	wa, err := s.webauthnForRequest(r)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	assertion, session, err := wa.BeginDiscoverableLogin()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "starting passkey login: "+err.Error())
		return
	}
	flowID, err := randomFlowID()
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.webauthn.putLogin(flowID, session)
	http.SetCookie(w, &http.Cookie{
		Name: webauthnLoginCookie, Value: flowID, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: int(webauthnCeremonyTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, assertion)
}

func (s *Server) handleWebAuthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(webauthnLoginCookie)
	if err != nil {
		httpError(w, http.StatusBadRequest, "no passkey login in progress (it may have expired) — try again")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: webauthnLoginCookie, Value: "", Path: "/", MaxAge: -1})
	session, ok := s.webauthn.takeLogin(cookie.Value)
	if !ok {
		httpError(w, http.StatusBadRequest, "no passkey login in progress (it may have expired) — try again")
		return
	}
	wa, err := s.webauthnForRequest(r)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ctx := r.Context()
	unknown := false
	user, credential, err := wa.FinishPasskeyLogin(s.webauthnDiscoverableHandler(ctx, &unknown), *session, r)
	if err != nil {
		if unknown {
			// 404, not 401: the browser offered a passkey this server has
			// no record of (removed from the profile page, or made against
			// a wiped dev database). login.html then tells the provider
			// via PublicKeyCredential.signalUnknownCredential, so it stops
			// offering it.
			httpError(w, http.StatusNotFound, "passkey login failed: unknown credential")
			return
		}
		httpError(w, http.StatusUnauthorized, "passkey login failed: "+err.Error())
		return
	}
	wu, ok := user.(webauthnUser)
	if !ok {
		httpError(w, http.StatusInternalServerError, "internal error resolving passkey owner")
		return
	}

	// Clone-detection flag (see migration 0029's doc comment and
	// Authenticator.UpdateCounter): the library computed this from
	// comparing the assertion's sign count against the stored one. It
	// doesn't reject the ceremony itself — many platform authenticators
	// legitimately never advance the counter past 0 — so this phase's
	// choice is to flag it loudly in the server log (for the admin to
	// notice and investigate) rather than lock the person out of their
	// own account on what may well be a false positive.
	if credential.Authenticator.CloneWarning {
		log.Printf("WEBAUTHN CLONE WARNING: credential for person %d (%s) reported a sign count that didn't advance — possible cloned authenticator", wu.person.ID, wu.person.Username)
	}

	if err := s.store.UpdateWebAuthnCredentialAfterLogin(ctx, credential.ID, credential.Authenticator.SignCount,
		credential.Flags.BackupEligible, credential.Flags.BackupState, credential.Flags.UserPresent, credential.Flags.UserVerified,
		passkeyDeviceLabel(r.UserAgent())); err != nil {
		log.Printf("updating passkey after login for person %d: %v", wu.person.ID, err)
	}

	// Same session-creation path as password/magic-link login (auth.go) —
	// deliberately not a separate mechanism.
	sessToken, err := s.store.CreateSession(ctx, wu.person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sessToken, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: 30 * 24 * 60 * 60,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "redirect": "/home"})
}

// webauthnDiscoverableHandler returns the webauthn.DiscoverableUserHandler
// FinishPasskeyLogin calls with the assertion's rawID/userHandle once it
// has one, to resolve which person is logging in. userHandle is the
// person id this phase encoded as WebAuthnID() at registration time (see
// webauthnUser); rawID is cross-checked against the credential actually
// stored for that person (GetWebAuthnCredentialByCredentialID), so a
// userHandle that doesn't actually own that credential id is rejected
// rather than trusted outright. *unknown is set when rawID isn't stored
// at all, so the caller can tell that apart from a bad signature.
func (s *Server) webauthnDiscoverableHandler(ctx context.Context, unknown *bool) webauthn.DiscoverableUserHandler {
	return func(rawID, userHandle []byte) (webauthn.User, error) {
		stored, err := s.store.GetWebAuthnCredentialByCredentialID(ctx, rawID)
		if err != nil {
			*unknown = errors.Is(err, store.ErrNotFound)
			return nil, err
		}
		personID, ok := personIDFromBytes(userHandle)
		if !ok || personID != stored.PersonID {
			return nil, errors.New("credential does not belong to the claimed account")
		}
		p, err := s.store.GetPersonByID(ctx, personID)
		if err != nil {
			return nil, err
		}
		creds, err := s.store.ListWebAuthnCredentials(ctx, personID)
		if err != nil {
			return nil, err
		}
		return webauthnUser{person: p, creds: creds}, nil
	}
}

// passkeyNicknameMax caps a typed passkey name; the profile input's
// maxlength says the same.
const passkeyNicknameMax = 60

// aaguidString formats a 16-byte AAGUID in canonical UUID text form for
// the uuid column, or "" when the authenticator sent none.
func aaguidString(b []byte) string {
	if len(b) != 16 {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// passkeyDeviceLabel turns a User-Agent into "Safari on iPhone" — the
// fallback name for a passkey whose AAGUID doesn't say which provider made
// it (all zeros, or not in store/passkey_providers.json). Only the coarse
// browser and platform: enough to tell two passkeys apart, nothing worth
// fingerprinting. Order matters: Edge, Opera and Samsung Internet also
// claim "Chrome", Chrome also claims "Safari", and every iOS browser is
// Safari underneath with its own token (CriOS, FxiOS, EdgiOS).
func passkeyDeviceLabel(ua string) string {
	has := func(tokens ...string) bool {
		for _, t := range tokens {
			if strings.Contains(ua, t) {
				return true
			}
		}
		return false
	}
	var browser string
	switch {
	case has("Edg/", "EdgA/", "EdgiOS/"):
		browser = "Edge"
	case has("OPR/", "OPT/"):
		browser = "Opera"
	case has("SamsungBrowser/"):
		browser = "Samsung Internet"
	case has("Firefox/", "FxiOS/"):
		browser = "Firefox"
	case has("Chrome/", "CriOS/"):
		browser = "Chrome"
	case has("Safari/"):
		browser = "Safari"
	}
	var platform string
	switch {
	case has("iPhone"):
		platform = "iPhone"
	case has("iPad"):
		platform = "iPad"
	case has("Android"):
		platform = "Android"
	case has("CrOS"):
		platform = "ChromeOS"
	case has("Windows"):
		platform = "Windows"
	case has("Macintosh", "Mac OS X"):
		platform = "Mac"
	case has("Linux"):
		platform = "Linux"
	}
	switch {
	case browser != "" && platform != "":
		return browser + " on " + platform
	case platform != "":
		return platform
	}
	return browser
}
