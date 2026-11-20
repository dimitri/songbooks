package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"app/internal/instruments"
	"app/internal/store"
)

// Instruments in the profile (migration 0101, internal/instruments): the
// "My instruments" card on /profile, what someone says they'll play when
// registering for a jam, and a JSON view of a person's instruments for
// the people allowed to see them.
//
// Who sees what (enforced in the store, levelsVisibleSQL):
//   - the instruments themselves: the person, app admins, people in a
//     band or community with them now, and managers of an event they
//     registered for (store.CanSeeInstruments); on an event's page,
//     what someone registered to play shows to whoever sees the page;
//   - their levels: the person and app admins; and, unless the person
//     turned sharing off, their current bandmates, the organisers
//     (manage members or events) of their communities, and managers of
//     the events they registered for. Never the public, never plain
//     fellow members of a community.

// instrumentsCard is the profile card's data.
type instrumentsCard struct {
	Mine   []store.PersonInstrument
	Shared bool
	Error  string
	// Saved is "" (nothing just saved), "sharing" (the sharing choice)
	// or "1" (an instrument).
	Saved string
}

func (s *Server) instrumentsCard(r *http.Request, personID int64) (instrumentsCard, error) {
	mine, err := s.store.ListPersonInstruments(r.Context(), personID)
	if err != nil {
		return instrumentsCard{}, err
	}
	shared, err := s.store.InstrumentLevelsShared(r.Context(), personID)
	if err != nil {
		return instrumentsCard{}, err
	}
	return instrumentsCard{
		Mine: mine, Shared: shared,
		Error: r.URL.Query().Get("insterror"),
		Saved: r.URL.Query().Get("instsaved"),
	}, nil
}

// backToInstruments is where the card's forms come back to.
func backToInstruments(w http.ResponseWriter, r *http.Request, errMsg string) {
	backToInstrumentsSaved(w, r, errMsg, "1")
}

// backToInstrumentsSaved is backToInstruments saying what was saved
// (instrumentsCard.Saved).
func backToInstrumentsSaved(w http.ResponseWriter, r *http.Request, errMsg, saved string) {
	dest := "/profile?instsaved=" + saved + "#instruments"
	if errMsg != "" {
		dest = "/profile?insterror=" + errMsg + "#instruments"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// handleAddInstrument is POST /profile/instruments/add: instrument (an
// instruments.All ID), other_name (for "other"), level (optional).
func (s *Server) handleAddInstrument(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	inst := r.FormValue("instrument")
	if !instruments.Valid(inst) {
		backToInstruments(w, r, "pick")
		return
	}
	otherName := strings.TrimSpace(r.FormValue("other_name"))
	if inst == instruments.Other && otherName == "" {
		backToInstruments(w, r, "other")
		return
	}
	if _, err := s.store.AddInstrument(r.Context(), p.ID, inst, otherName, r.FormValue("level")); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	backToInstruments(w, r, "")
}

// handleSetInstrumentLevel is POST /profile/instruments/{iid}/level.
func (s *Server) handleSetInstrumentLevel(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	id, err := pathID(r, "iid")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	level := r.FormValue("level")
	if level != "" && !instruments.ValidLevel(level) {
		httpError(w, http.StatusBadRequest, "unknown level")
		return
	}
	if err := s.store.SetInstrumentLevel(r.Context(), p.ID, id, level); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	backToInstruments(w, r, "")
}

// handleRemoveInstrument is POST /profile/instruments/{iid}/remove.
func (s *Server) handleRemoveInstrument(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	id, err := pathID(r, "iid")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.RemoveInstrument(r.Context(), p.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	backToInstruments(w, r, "")
}

// handleShareInstrumentLevels is POST /profile/instruments/sharing:
// share=on lets bandmates and organisers see the levels; without it,
// only the person and the app's admins do.
func (s *Server) handleShareInstrumentLevels(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	if err := s.store.SetInstrumentLevelsShared(r.Context(), p.ID, r.FormValue("share") != ""); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	backToInstrumentsSaved(w, r, "", "sharing")
}

// personInstrumentJSON is one instrument in GET /api/people/{id}/instruments.
type personInstrumentJSON struct {
	Instrument string `json:"instrument"`
	Name       string `json:"name"`
	Stem       string `json:"stem"`
	Level      string `json:"level,omitempty"`
	Source     string `json:"source"`
	SourceName string `json:"sourceName,omitempty"`
}

// handlePersonInstruments is GET /api/people/{id}/instruments: the
// person's instruments, with levels only where the viewer may see them;
// 404 for a viewer who may not see the instruments at all.
func (s *Server) handlePersonInstruments(w http.ResponseWriter, r *http.Request) {
	viewer := personFromContext(r)
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ok, err := s.store.CanSeeInstruments(r.Context(), viewer.ID, id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		httpError(w, http.StatusNotFound, "no such person")
		return
	}
	m, err := s.store.InstrumentsSeenBy(r.Context(), viewer.ID, []int64{id})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lang := s.langOfRequest(r)
	out := []personInstrumentJSON{}
	for _, pi := range m[id] {
		out = append(out, personInstrumentJSON{
			Instrument: pi.Instrument, Name: pi.Name(lang), Stem: instruments.Stem(pi.Instrument),
			Level: pi.Level, Source: pi.Source, SourceName: pi.SourceName,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"personId": id, "instruments": out})
}

// playingEventTypes are the events people come to play at together:
// their registration asks what one will play. (A lesson or a master
// class is the coach's to plan, not a line-up.)
var playingEventTypes = map[string]bool{"jam": true, "open_mic": true, "show_together": true}

// eventPlay is one instrument someone registered to play, for the
// event page's "Who's coming".
type eventPlay struct {
	Instrument string
	Level      string // "" unless the viewer may see it
}

// eventPlays maps each attendee to what they registered to play, with
// their level on it where viewer may see levels.
func (s *Server) eventPlays(r *http.Request, viewerID int64, attendees []store.EventRegistration) (map[int64][]eventPlay, error) {
	var ids []int64
	for _, a := range attendees {
		if len(a.Instruments) > 0 {
			ids = append(ids, a.PersonID)
		}
	}
	seen, err := s.store.InstrumentsSeenBy(r.Context(), viewerID, ids)
	if err != nil {
		return nil, err
	}
	out := map[int64][]eventPlay{}
	for _, a := range attendees {
		levels := map[string]string{}
		for _, pi := range seen[a.PersonID] {
			levels[pi.Instrument] = pi.Level
		}
		for _, inst := range a.Instruments {
			out[a.PersonID] = append(out[a.PersonID], eventPlay{inst, levels[inst]})
		}
	}
	return out, nil
}

// commonPlays are offered on every playing event's registration, ticked
// or not, besides the person's own instruments.
var commonPlays = []string{"voice", "guitar", "bass", "drums", "keys"}

// playChoices is the instruments a registration form offers as
// checkboxes: the person's own (mine) and the common ones.
func playChoices(mine []string) []string {
	out := append([]string{}, commonPlays...)
	for _, id := range mine {
		if !stringSet(out)[id] {
			out = append(out, id)
		}
	}
	instruments.Sort(out)
	return out
}

func stringSet(list []string) map[string]bool {
	m := map[string]bool{}
	for _, v := range list {
		m[v] = true
	}
	return m
}

// saveEventPlays saves what a registration form says personID will play
// (its "instrument" checkboxes and the "instrument_more" picker), when
// the form had that question ("plays").
func (s *Server) saveEventPlays(r *http.Request, eventID, personID int64) error {
	if r.FormValue("plays") == "" {
		return nil
	}
	ids := append([]string{}, r.Form["instrument"]...)
	if more := r.FormValue("instrument_more"); more != "" {
		ids = append(ids, more)
	}
	return s.store.SetEventRegistrationInstruments(r.Context(), eventID, personID, ids)
}

// handleSetEventInstruments is POST /events/{id}/instruments: a
// registered person changing what they'll play.
func (s *Server) handleSetEventInstruments(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	eventID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	ids := append([]string{}, r.Form["instrument"]...)
	if more := r.FormValue("instrument_more"); more != "" {
		ids = append(ids, more)
	}
	if err := s.store.SetEventRegistrationInstruments(r.Context(), eventID, p.ID, ids); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusForbidden, "register for this event first")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/events/"+strconv.FormatInt(eventID, 10), http.StatusSeeOther)
}
