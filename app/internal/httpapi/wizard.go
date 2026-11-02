package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"app/internal/store"
)

// handleWelcomePage is GET /welcome — the getting-started wizard. A
// single page with a ?step=N query param rather than five routes: every
// step reads real data (instruments, band membership, songbook entries)
// to decide whether it's already done, so there's no wizard-specific
// state to thread through separate URLs beyond "which step am I showing
// right now." Re-entering at any time (from /profile's "Setup" link)
// shows this same already-done state instead of blanking it out — see
// migration 0031's comment for why onboarded_at itself is untouched by
// a voluntary re-entry.
func (s *Server) handleWelcomePage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	step, _ := strconv.Atoi(r.URL.Query().Get("step"))
	if step < 1 || step > 5 {
		step = 1
	}
	next := r.URL.Query().Get("next")

	data := map[string]any{"Me": person, "Step": step, "Next": next}

	switch step {
	case 2:
		checked := map[string]bool{}
		var other []string
		for _, inst := range person.Instruments {
			if isCommonInstrument(inst) {
				checked[inst] = true
			} else {
				other = append(other, inst)
			}
		}
		data["CommonInstruments"] = commonInstruments
		data["Checked"] = checked
		data["Other"] = strings.Join(other, ", ")
		data["AlreadyDone"] = len(person.Instruments) > 0
	case 3:
		bands, err := s.store.ListBandsForPerson(r.Context(), person.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data["Bands"] = bands
	case 4:
		mine, err := s.store.ListMyCatalog(r.Context(), person.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data["MyCatalogCount"] = len(mine)
		all, err := s.store.ListCatalog(r.Context(), person.ID, store.CatalogFilter{})
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		mineIDs := map[int64]bool{}
		for _, e := range mine {
			if e.ForkedFromID != nil {
				mineIDs[*e.ForkedFromID] = true
			}
		}
		var picks []any
		for _, v := range all {
			if len(picks) >= 8 {
				break
			}
			if mineIDs[v.VariantID] {
				continue
			}
			picks = append(picks, v)
		}
		data["Picks"] = picks
	}

	s.render(w, "welcome", data)
}

// handlePostWelcomeFinish is POST /welcome/finish — reached either by
// "Done" on the last step or "skip the rest of setup" from any step
// (both mean the same thing: stop offering this automatically from now
// on). Never reached by a voluntary re-entry from /profile finishing
// normally — that path also posts here, which just re-stamps
// onboarded_at, harmless (see MarkOnboarded's doc comment).
func (s *Server) handlePostWelcomeFinish(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	if err := s.store.MarkOnboarded(r.Context(), person.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dest := r.FormValue("next")
	if dest == "" {
		dest = "/"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}
