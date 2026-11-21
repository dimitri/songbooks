package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"app/internal/store"
)

// A site admin's "Login link" on a members page: a fresh magic-link
// login token for one member, shown to the admin as a link and a QR code
// (to send it, or to hold a screen up at an event for the member to
// scan), or mailed to the member instead. Same token, same 15 minutes,
// same single use and same /login/confirm as the login page's email.
//
// Admin only (requireAdmin, checked here on every route), never for a
// shell person (a name on a roster with no account yet: their personal
// invite link is what they need). Each link created or sent is logged,
// who for whom, never the token.

// loginLinkTarget resolves {personID} for the routes below: 400 on a bad
// id, 404 when nobody has it, 409 for a shell person. It writes the
// error response itself and returns nil then.
func (s *Server) loginLinkTarget(w http.ResponseWriter, r *http.Request) *store.Person {
	if !sameOrigin(r) {
		httpError(w, http.StatusForbidden, "cross-origin request")
		return nil
	}
	id, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return nil
	}
	p, err := s.store.GetPersonByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such person")
		return nil
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil
	}
	shell, err := s.store.IsShellPerson(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil
	}
	if shell {
		httpError(w, http.StatusConflict, "this person has no account yet")
		return nil
	}
	return p
}

// sameOrigin refuses a request a browser says came from another site:
// Sec-Fetch-Site when the browser sends it, else Origin. The session
// cookie is SameSite=Lax already; this is the belt to those braces for
// a route that hands out a way in to someone else's account. A request
// with neither header (curl, the tests) passes: it carries no ambient
// cookie of a victim's either.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host
	}
	return true
}

// loginLinkResponse is what the dialog shows.
type loginLinkResponse struct {
	URL         string `json:"url"`
	Token       string `json:"token"`
	SecondsLeft int    `json:"secondsLeft"`
	Name        string `json:"name"`
	IsAdmin     bool   `json:"isAdmin"`
	HasEmail    bool   `json:"hasEmail"`
	QRSVG       string `json:"qrSvg"`
}

// handleAdminCreateLoginLink is POST /api/admin/people/{personID}/login-link.
func (s *Server) handleAdminCreateLoginLink(w http.ResponseWriter, r *http.Request) {
	p := s.loginLinkTarget(w, r)
	if p == nil {
		return
	}
	token, err := s.store.CreateLoginToken(r.Context(), p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	link := s.loginLinkURL(r, token)
	// The QR code is drawn here and sent back with the link, rather than
	// fetched from /api/qr?data=…: the token never sits in a URL, where
	// access logs and browser history would keep it.
	svg, err := qrSVG(link)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	admin := personFromContext(r)
	log.Printf("login link: admin %s (id %d) created a login link for %s (id %d)", admin.Username, admin.ID, p.Username, p.ID)
	writeJSON(w, http.StatusOK, loginLinkResponse{
		URL: link, Token: token, SecondsLeft: int(store.LoginTokenTTL / time.Second),
		Name: p.DisplayName, IsAdmin: p.IsAdmin, HasEmail: p.Email != "", QRSVG: svg,
	})
}

// handleAdminSendLoginLink is POST /api/admin/people/{personID}/login-link/email:
// a fresh link, mailed to the member with the login page's own email,
// so the admin never sees it. {"revoke": token} also ends the link the
// admin had on screen, so only the emailed one works.
func (s *Server) handleAdminSendLoginLink(w http.ResponseWriter, r *http.Request) {
	p := s.loginLinkTarget(w, r)
	if p == nil {
		return
	}
	var req struct {
		Revoke string `json:"revoke"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	if p.Email == "" {
		httpError(w, http.StatusConflict, "this person has no email address")
		return
	}
	if req.Revoke != "" {
		if err := s.store.RevokeLoginToken(r.Context(), req.Revoke, p.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	token, err := s.store.CreateLoginToken(r.Context(), p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.sendLoginEmail(r, p, s.loginLinkURL(r, token)); err != nil {
		log.Printf("sending login email: %v", err)
		httpError(w, http.StatusBadGateway, "couldn't send the email")
		return
	}
	admin := personFromContext(r)
	log.Printf("login link: admin %s (id %d) emailed a login link to %s (id %d)", admin.Username, admin.ID, p.Username, p.ID)
	writeJSON(w, http.StatusOK, map[string]any{"sent": true})
}

// handleAdminLoginLinkStatus is POST /api/admin/people/{personID}/login-link/status
// with {"token": …}: has the link on the admin's screen been used, and
// how long is left. POST, so the token stays out of URLs.
func (s *Server) handleAdminLoginLinkStatus(w http.ResponseWriter, r *http.Request) {
	p := s.loginLinkTarget(w, r)
	if p == nil {
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		httpError(w, http.StatusBadRequest, "missing token")
		return
	}
	st, err := s.store.GetLoginTokenState(r.Context(), req.Token)
	if errors.Is(err, store.ErrNotFound) || (err == nil && st.PersonID != p.ID) {
		httpError(w, http.StatusNotFound, "no such link")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	left := int(time.Until(st.ExpiresAt) / time.Second)
	if left < 0 {
		left = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{"used": st.Used, "secondsLeft": left})
}

// qrSVG draws data as a QR code, one SVG path of black modules on a
// white square with the standard quiet zone: sharp at any size, from a
// phone's dialog to a laptop held up full screen.
func qrSVG(data string) (string, error) {
	q, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bm := q.Bitmap()
	n := len(bm)
	var d strings.Builder
	for y, row := range bm {
		for x := 0; x < len(row); {
			if !row[x] {
				x++
				continue
			}
			start := x
			for x < len(row) && row[x] {
				x++
			}
			fmt.Fprintf(&d, "M%d %dh%dv1h-%dz", start, y, x-start, x-start)
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="%s"/></svg>`,
		n, n, n, n, d.String()), nil
}
