package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"app/internal/store"
)

// commonInstruments is the icon-picker/profile vocabulary this phase
// reuses everywhere an instrument is picked — the same four defaults
// DefaultInstrumentLimits() starts a jam song's caps from. Anything else
// a person plays (or a jam participant picks) is free text, not
// validated against this list.
var commonInstruments = []string{"drummer", "bassist", "guitarist", "singer"}

// handleProfilePage is GET /profile — a logged-in person's own account
// settings. The only setting this phase adds is "which instruments do
// you play" (Person.Instruments), used to pre-fill jam signup; there was
// no profile/account-settings page at all before this phase, so this is
// a brand-new page, not an addition to an existing one.
func (s *Server) handleProfilePage(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	checked := map[string]bool{}
	var other []string
	for _, inst := range p.Instruments {
		if isCommonInstrument(inst) {
			checked[inst] = true
		} else {
			other = append(other, inst)
		}
	}
	passkeys, err := s.store.ListWebAuthnCredentials(r.Context(), p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "profile", map[string]any{
		"Me": p, "CommonInstruments": commonInstruments, "Checked": checked,
		"Other":       strings.Join(other, ", "),
		"HasPassword": p.PasswordHash != "",
		"PwError":     r.URL.Query().Get("pwerror"),
		"PwSaved":     r.URL.Query().Get("pwsaved") != "",
		"Passkeys":    passkeys,
		"EmailError":  r.URL.Query().Get("emailerror"),
		"EmailSaved":  r.URL.Query().Get("emailsaved") != "",
		"PublicSaved": r.URL.Query().Get("publicsaved") != "",
		"Tab":         r.URL.Query().Get("tab"),
	})
}

// handleSetPublicProfile is POST /profile/public — saves the "public
// profile" tab's fields (see migration 0038): a nickname/picture to show
// instead of the real username, plus opt-in toggles for whether email/
// phone show on that public view. There's no public-facing page that
// actually reads these yet — this is the editor half only, ahead of
// whatever surface (a public catalog, a jam roster) ends up showing it.
func (s *Server) handleSetPublicProfile(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	nickname := strings.TrimSpace(r.FormValue("nickname"))
	pictureURL := strings.TrimSpace(r.FormValue("picture_url"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	shareEmail := r.FormValue("share_email") == "on"
	sharePhone := r.FormValue("share_phone") == "on"
	if err := s.store.SetPublicProfile(r.Context(), p.ID, nickname, pictureURL, phone, shareEmail, sharePhone); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/profile?tab=public&publicsaved=1", http.StatusFound)
}

// handleSetEmailProfile is POST /profile/email — lets a logged-in person
// set or change the email their account uses for magic-link login (and,
// going forward, as their login identifier for password login too — see
// handlePasswordLogin, which already accepts either username or email).
// Many pre-existing accounts (e.g. the --import bootstrap path) start
// with no email at all and are otherwise stuck on password-only login —
// this is the self-serve path to fix that, alongside the --set-email CLI
// flag it shares SetEmail with.
func (s *Server) handleSetEmailProfile(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/profile?emailerror=invalid+form", http.StatusFound)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	// Deliberately minimal validation (non-empty, contains "@" with
	// something on each side) — same posture as the rest of this app's
	// forms (e.g. the invite-accept form's email field): a stricter RFC
	// validator would reject real addresses it doesn't recognize, and the
	// address only needs to be deliverable, which actually sending to it
	// is the real test of, not a regex here.
	at := strings.IndexByte(email, '@')
	if email == "" || at <= 0 || at == len(email)-1 {
		http.Redirect(w, r, "/profile?emailerror=enter+a+valid+email+address", http.StatusFound)
		return
	}
	if err := s.store.SetEmail(r.Context(), p.ID, email); err != nil {
		if errors.Is(err, store.ErrEmailTaken) {
			http.Redirect(w, r, "/profile?emailerror=that+email+is+already+registered+to+another+account", http.StatusFound)
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/profile?emailsaved=1", http.StatusFound)
}

// handleSetPasswordProfile is POST /profile/password — lets a logged-in
// person set or change their own password, the "opt into password login
// later" path for an account that was created magic-link-only (see
// store.CreatePersonAndAcceptInvite's bcryptHash param for the "set it at
// invite-accept time" path instead). Requires the new password typed
// twice (basic typo protection) and a minimum length; any failure
// redirects back to /profile with a short reason rather than a generic
// error, since there's no account-enumeration concern here — the visitor
// is already identified via requireAuth.
func (s *Server) handleSetPasswordProfile(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/profile?pwerror=invalid+form", http.StatusFound)
		return
	}
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")
	if len(password) < 8 {
		http.Redirect(w, r, "/profile?pwerror=password+must+be+at+least+8+characters", http.StatusFound)
		return
	}
	if password != confirm {
		http.Redirect(w, r, "/profile?pwerror=passwords+didn%27t+match", http.StatusFound)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.SetPassword(r.Context(), p.ID, string(hash)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/profile?pwsaved=1", http.StatusFound)
}

func isCommonInstrument(inst string) bool {
	for _, c := range commonInstruments {
		if c == inst {
			return true
		}
	}
	return false
}

// handleUpdateInstruments is POST /profile/instruments — saves the
// checked common instruments plus any free-text "other" ones (comma
// separated), replacing Person.Instruments wholesale (not a diff/merge —
// simplest correct behavior for a small checkbox+text form).
func (s *Server) handleUpdateInstruments(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	var instruments []string
	seen := map[string]bool{}
	for _, inst := range r.Form["instrument"] {
		inst = strings.TrimSpace(inst)
		if inst == "" || seen[inst] {
			continue
		}
		seen[inst] = true
		instruments = append(instruments, inst)
	}
	for _, inst := range strings.Split(r.FormValue("other"), ",") {
		inst = strings.TrimSpace(inst)
		if inst == "" || seen[inst] {
			continue
		}
		seen[inst] = true
		instruments = append(instruments, inst)
	}
	if err := s.store.SetInstruments(r.Context(), p.ID, instruments); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// "next" lets the getting-started wizard (wizard.go) reuse this same
	// handler for its instrument step instead of a duplicate one —
	// absent, this is just the plain /profile form.
	dest := r.FormValue("next")
	if dest == "" {
		dest = "/profile"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}
