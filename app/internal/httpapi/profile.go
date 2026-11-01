package httpapi

import (
	"net/http"
	"strings"
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
	s.render(w, "profile", map[string]any{
		"Me": p, "CommonInstruments": commonInstruments, "Checked": checked,
		"Other": strings.Join(other, ", "),
	})
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
	http.Redirect(w, r, "/profile", http.StatusFound)
}
