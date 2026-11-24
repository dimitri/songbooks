package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"app/internal/instruments"
	"app/internal/store"
)

// commonInstruments are the welcome wizard's instrument icons
// (canonical IDs, internal/instruments); anything else is typed in its
// "other" field and read with instruments.Parse.
var commonInstruments = []string{"voice", "guitar", "bass", "drums", "keys"}

// commonChecked splits a person's instruments (their profile rows) into
// the common ones, ticked on the wizard's icons, and the names (in lang)
// of the rest, which the wizard lists as already on the profile: it
// never takes those off.
func commonChecked(list []store.PersonInstrument, lang string) (map[string]bool, []string) {
	checked := map[string]bool{}
	var rest []string
	for _, pi := range list {
		if pi.Instrument != instruments.Other && isCommonInstrument(pi.Instrument) {
			checked[pi.Instrument] = true
			continue
		}
		rest = append(rest, pi.Name(lang))
	}
	return checked, rest
}

// handleProfilePage is GET /profile — a logged-in person's own account
// settings. The only setting this phase adds is "which instruments do
// you play" (Person.Instruments), used to pre-fill jam signup; there was
// no profile/account-settings page at all before this phase, so this is
// a brand-new page, not an addition to an existing one.
func (s *Server) handleProfilePage(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	card, err := s.instrumentsCard(r, p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	passkeys, err := s.store.ListWebAuthnCredentials(r.Context(), p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	locale, _ := s.store.PersonLocale(r.Context(), p.ID)
	s.render(w, "profile", map[string]any{
		"Me": p, "Instruments": card,
		"HasPassword": p.PasswordHash != "",
		"LoginEmails": s.loginEmails(),
		"PwError":     r.URL.Query().Get("pwerror"),
		"PwSaved":     r.URL.Query().Get("pwsaved") != "",
		"Passkeys":    passkeys,
		"EmailError":  r.URL.Query().Get("emailerror"),
		"EmailSaved":  r.URL.Query().Get("emailsaved") != "",
		"PublicSaved": r.URL.Query().Get("publicsaved") != "",
		"Tab":         r.URL.Query().Get("tab"),
		"Locale":      locale,
	})
}

// handleSetPublicProfile is POST /profile/public — saves the "public
// profile" tab's fields (see migration 0038): a nickname/picture to show
// instead of the real name, plus opt-in toggles for whether email/
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
	gravatarOptOut := r.FormValue("gravatar_opt_out") == "on"
	if err := s.store.SetPublicProfile(r.Context(), p.ID, nickname, pictureURL, phone, shareEmail, sharePhone, gravatarOptOut); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/profile?tab=public&publicsaved=1", http.StatusFound)
}

// handleSetEmailProfile is POST /profile/email — lets a logged-in person
// set or change the email their account is: what they log in with, by
// password (handlePasswordLogin) or by magic link — there is no login
// name (docs/accounts.md). Changing it changes how they log in from now
// on; the --set-email CLI flag shares SetEmail with it, for an account
// that has none and so can't log in to get here.
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
	if msg := newPasswordProblem(password, confirm); msg != "" {
		http.Redirect(w, r, "/profile?pwerror="+url.QueryEscape(msg), http.StatusFound)
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

// handleUpdateInstruments is POST /profile/instruments, the welcome
// wizard's "What do you play?": the ticked icons ("instrument") and the
// comma-separated "other" field are added to the profile; an icon that
// was ticked when the page was drawn ("had") and is now unticked is taken
// off. Nothing else is: not the instruments the wizard listed as already
// there, not ones collected from a band or an event in the meantime.
func (s *Server) handleUpdateInstruments(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	var add []string
	ticked := map[string]bool{}
	for _, inst := range r.Form["instrument"] {
		inst = strings.TrimSpace(inst)
		if inst == "" || ticked[inst] {
			continue
		}
		ticked[inst] = true
		add = append(add, inst)
	}
	for _, inst := range strings.Split(r.FormValue("other"), ",") {
		if inst = strings.TrimSpace(inst); inst != "" {
			add = append(add, inst)
		}
	}
	var takeOff []string
	for _, inst := range r.Form["had"] {
		if isCommonInstrument(inst) && !ticked[inst] {
			takeOff = append(takeOff, inst)
		}
	}
	if err := s.store.UpdateOwnInstruments(r.Context(), p.ID, add, takeOff); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// "next" brings the wizard (wizard.go) on to its next step.
	dest := r.FormValue("next")
	if dest == "" {
		dest = "/profile#instruments"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}
