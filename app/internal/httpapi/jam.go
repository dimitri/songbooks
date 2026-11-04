package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"app/internal/store"
)

const jamCookie = "songbooks_jam"

// jamTTL is how long a jam event stays open for signups after creation —
// a single evening plus some slack, not a standing fixture.
const jamTTL = 18 * time.Hour

// handleCreateJamEvent starts a new jam event drawing from songbook {id}.
func (s *Server) handleCreateJamEvent(w http.ResponseWriter, r *http.Request) {
	songbookID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanAccessSongbook(r.Context(), p.ID, songbookID); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no access to this songbook")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Jam night"
	}
	ev, err := s.store.CreateJamEvent(r.Context(), songbookID, name, jamTTL, p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/songbooks/"+strconv.FormatInt(songbookID, 10)+"/jam/"+ev.Code+"/roster", http.StatusFound)
}

// handleJamOrganizerJoin is the separate "become an organizer" link
// (GET /jam-organize/{token} — see migration 0045's doc comment for why
// it's a distinct token/route from the plain participant link, never the
// same page): requireAuth-gated (an organizer must already have a real
// account, per the user's own spec — no anonymous-profile path here at
// all), adds the visitor as an organizer of the jam this token belongs
// to, then sends them to the roster.
func (s *Server) handleJamOrganizerJoin(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	ev, err := s.store.GetJamEventByOrganizerToken(r.Context(), token)
	if err != nil {
		http.Error(w, "this organizer link is invalid or has expired", http.StatusNotFound)
		return
	}
	p := personFromContext(r)
	if err := s.store.AddJamOrganizer(r.Context(), ev.ID, p.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/songbooks/"+strconv.FormatInt(ev.SongbookID, 10)+"/jam/"+ev.Code+"/roster", http.StatusFound)
}

// handleJamsIndexPage is the "Jam sessions" nav destination: every still-
// open jam this person organizes or has joined (account-linked), plus a
// way to start a new one from any songbook they can reach.
func (s *Server) handleJamsIndexPage(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	jams, err := s.store.ListJamEventsForPerson(r.Context(), p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mySongbooks, err := s.store.ListSongbooks(r.Context(), "person", p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	bands, err := s.store.ListBandsForPerson(r.Context(), p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var bandSongbooks []store.Songbook
	for _, b := range bands {
		sbs, err := s.store.ListSongbooks(r.Context(), "band", b.ID)
		if err != nil {
			continue
		}
		bandSongbooks = append(bandSongbooks, sbs...)
	}
	s.render(w, "jams_index", map[string]any{
		"Me": p, "Jams": jams, "MySongbooks": mySongbooks, "BandSongbooks": bandSongbooks,
	})
}

// handleJamLandingPage is the public (no login) page a jam code's link
// opens: event name plus a nickname+instrument signup form. Reached via
// optionalPerson (router.go), not requireAuth — jam signup itself stays
// unlogged-nickname-only (see handleJamJoin), but a person who HAPPENS
// to also be logged in in this same browser gets their own saved
// instruments (Person.Instruments, set on /profile) passed through so
// the form can pre-select their usual instrument rather than starting
// blank — see jam_landing.html's icon picker.
// handleJamLandingPage either asks for a nickname (anonymous visitor) or,
// per the user's own decision ("log in first, then the link just
// works"), skips the form entirely when the visitor already has a
// session: joins immediately under their own display name, reusing
// their existing profile for this jam if they've joined it before (see
// CreateJamProfile), and lands them straight on the song list.
func (s *Server) handleJamLandingPage(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		http.Error(w, "this jam link is invalid or has expired", http.StatusNotFound)
		return
	}
	if p := personFromContext(r); p != nil {
		if err := s.joinJamAsPerson(w, r, ev, p.ID, p.DisplayName); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		http.Redirect(w, r, "/jam/"+code+"/songs", http.StatusFound)
		return
	}
	s.render(w, "jam_landing", map[string]any{
		"Code": code, "Name": ev.Name,
		"CommonInstruments": commonInstruments, "MyInstruments": []string(nil),
	})
}

// joinJamAsPerson creates/reuses a jam profile and sets the jam cookie —
// shared by the logged-in auto-join path (handleJamLandingPage) and the
// anonymous nickname-form path (handleJamJoin). personID is 0 for an
// anonymous participant.
func (s *Server) joinJamAsPerson(w http.ResponseWriter, r *http.Request, ev *store.JamEvent, personID int64, nickname string) error {
	profile, err := s.store.CreateJamProfile(r.Context(), ev.ID, nickname, personID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		// Path "/" (not scoped to "/jam/"+code, as this started as) —
		// widened by the jam-mode phase (final phase of the data-model
		// rewrite) so this same cookie is sent on /api/variants/... calls
		// too (e.g. the section-annotation endpoints, which a jam
		// participant needs to reach but which aren't themselves nested
		// under /jam/{code}/...). A deliberate, documented deviation from
		// the original "jam access is deliberately narrower" framing: the
		// profile id inside the cookie is still checked against the
		// specific event it claims (see jamProfileFromRequest/
		// CanEditVariantAsJamParticipant), so widening the Path alone
		// grants no extra access — it only changes which requests the
		// browser bothers attaching the cookie to.
		Name: jamCookie, Value: strconv.FormatInt(profile.ID, 10),
		Path: "/", HttpOnly: true, Secure: r.TLS != nil,
		SameSite: http.SameSiteLaxMode, MaxAge: int(jamTTL.Seconds()),
	})
	return nil
}

// handleJamJoin creates a jam profile (no password — a nickname is the
// whole identity) and sets the jam cookie identifying it, separate from
// the normal login session cookie, since this is a different, narrower
// kind of access. Only reached by an anonymous visitor in practice —
// handleJamLandingPage short-circuits straight through this for anyone
// already logged in.
func (s *Server) handleJamJoin(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		http.Error(w, "this jam link is invalid or has expired", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	nickname := strings.TrimSpace(r.FormValue("nickname"))
	if nickname == "" {
		http.Error(w, "a nickname is required", http.StatusBadRequest)
		return
	}
	var personID int64
	if p := personFromContext(r); p != nil {
		personID = p.ID
	}
	if err := s.joinJamAsPerson(w, r, ev, personID, nickname); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/jam/"+code+"/songs", http.StatusFound)
}

// jamProfileFromCookie resolves the jam cookie to its *store.JamProfile,
// with NO check yet against any specific event — just "does this request
// carry a jam session for SOME event, and does that profile still
// exist." Callers that care about a specific event (almost all of them)
// should use jamProfileFromRequest instead; this lower-level helper
// exists for the handful of endpoints (section annotations) that aren't
// addressed by jam code/event at all, and must resolve the event from the
// profile itself.
func (s *Server) jamProfileFromCookie(r *http.Request) *store.JamProfile {
	cookie, err := r.Cookie(jamCookie)
	if err != nil {
		return nil
	}
	id, err := strconv.ParseInt(cookie.Value, 10, 64)
	if err != nil {
		return nil
	}
	profile, err := s.store.GetJamProfile(r.Context(), id)
	if err != nil {
		return nil
	}
	return profile
}

// jamProfileFromRequest resolves the jam cookie to a *store.JamProfile for
// {code}, or nil if there isn't one (or it doesn't match this event) — the
// caller decides what to do (usually: redirect back to the landing page).
// This is deliberately separate from requireAuth/personFromContext: jam
// access is a different, narrower kind of identity, not a login.
func (s *Server) jamProfileFromRequest(r *http.Request, ev *store.JamEvent) *store.JamProfile {
	profile := s.jamProfileFromCookie(r)
	if profile == nil || profile.JamEventID != ev.ID {
		return nil
	}
	return profile
}

type jamCtxKey int

const (
	jamEventCtxKey   jamCtxKey = 0
	jamProfileCtxKey jamCtxKey = 1
)

func jamEventFromContext(r *http.Request) *store.JamEvent {
	ev, _ := r.Context().Value(jamEventCtxKey).(*store.JamEvent)
	return ev
}

func jamProfileFromContext(r *http.Request) *store.JamProfile {
	p, _ := r.Context().Value(jamProfileCtxKey).(*store.JamProfile)
	return p
}

// requireJamProfile gates a /jam/{code}/... participant endpoint on
// holding a valid jam session for THIS event — the jam-mode equivalent of
// requireAuth. On success, both the resolved *store.JamEvent and
// *store.JamProfile are stashed in context (jamEventFromContext/
// jamProfileFromContext) so handlers don't have to re-resolve the event.
func (s *Server) requireJamProfile(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		ev, err := s.store.GetJamEvent(r.Context(), code)
		if err != nil {
			httpError(w, http.StatusNotFound, "this jam link is invalid or has expired")
			return
		}
		profile := s.jamProfileFromRequest(r, ev)
		if profile == nil {
			if r.Method == http.MethodGet {
				http.Redirect(w, r, "/jam/"+code, http.StatusFound)
			} else {
				httpError(w, http.StatusUnauthorized, "join this jam first")
			}
			return
		}
		ctx := context.WithValue(r.Context(), jamEventCtxKey, ev)
		ctx = context.WithValue(ctx, jamProfileCtxKey, profile)
		next(w, r.WithContext(ctx))
	}
}

// defaultInstrumentFormRows is the shape the add-song form's prefilled
// rows take — one per store.DefaultInstrumentLimits() entry, plus a few
// blank rows for organizer-added instruments (keyboard, horns, etc., with
// no default caps, per the brief).
type instrumentFormRow struct {
	Instrument string
	Min        int
	Max        string // "" renders as an empty max input (unspecified/unlimited)
}

func defaultInstrumentFormRows() []instrumentFormRow {
	var rows []instrumentFormRow
	for _, l := range store.DefaultInstrumentLimits() {
		max := ""
		if l.MaxCount != nil {
			max = strconv.Itoa(*l.MaxCount)
		}
		rows = append(rows, instrumentFormRow{Instrument: l.Instrument, Min: l.MinCount, Max: max})
	}
	for i := 0; i < 3; i++ {
		rows = append(rows, instrumentFormRow{})
	}
	return rows
}

// handleJamAddSongsPage is the organizer's "add a song to this jam" page:
// every project song not yet on the jam's pickable list, each with an
// add-with-limits form prefilled with the organizer-facing defaults.
// Editor-or-owner gated (requireRole in router.go) — adding songs/caps is
// an organizer decision, not something a mere viewer should do.
func (s *Server) handleJamAddSongsPage(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		http.Error(w, "this jam link is invalid or has expired", http.StatusNotFound)
		return
	}
	candidates, err := s.store.ListAddableJamSongs(r.Context(), ev.ID, ev.SongbookID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "jam_add_songs", map[string]any{
		"SongbookID": ev.SongbookID, "Code": code, "Name": ev.Name,
		"Candidates": candidates, "DefaultRows": defaultInstrumentFormRows(),
		"CommonInstruments": commonInstruments,
	})
}

// handleAddJamSong adds one song to the jam's pickable list with the
// per-instrument min/max caps submitted from handleJamAddSongsPage's form
// — repeated instrument[]/min[]/max[] fields, one triple per row (blank
// instrument rows are skipped, so the organizer doesn't have to delete
// unused extra rows).
func (s *Server) handleAddJamSong(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		httpError(w, http.StatusNotFound, "this jam link is invalid or has expired")
		return
	}
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	songID, err := strconv.ParseInt(r.FormValue("song_id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song id")
		return
	}

	instruments, mins, maxes := r.Form["instrument"], r.Form["min"], r.Form["max"]
	var limits []store.InstrumentLimit
	for i, instrument := range instruments {
		instrument = strings.TrimSpace(instrument)
		if instrument == "" {
			continue
		}
		min := 0
		if i < len(mins) {
			if v, err := strconv.Atoi(strings.TrimSpace(mins[i])); err == nil {
				min = v
			}
		}
		var max *int
		if i < len(maxes) {
			if s := strings.TrimSpace(maxes[i]); s != "" {
				if v, err := strconv.Atoi(s); err == nil {
					max = &v
				}
			}
		}
		limits = append(limits, store.InstrumentLimit{Instrument: instrument, MinCount: min, MaxCount: max})
	}

	if err := s.store.AddJamSong(r.Context(), ev.ID, songID, limits); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/songbooks/"+strconv.FormatInt(ev.SongbookID, 10)+"/jam/"+code+"/roster", http.StatusFound)
}

// handleRemoveJamSong drops a song from the jam's pickable list.
func (s *Server) handleRemoveJamSong(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		httpError(w, http.StatusNotFound, "this jam link is invalid or has expired")
		return
	}
	songID, err := strconv.ParseInt(r.PathValue("songID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song id")
		return
	}
	if err := s.store.RemoveJamSong(r.Context(), ev.ID, songID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/songbooks/"+strconv.FormatInt(ev.SongbookID, 10)+"/jam/"+code+"/roster", http.StatusFound)
}

// handleJamSongsPage lists the jam event's pickable songs for the
// signed-up nickname to pick from — requires the jam cookie, not a normal
// login. The song list itself comes from ListJamSongs, a plain
// project-scoped query (see store/jam.go).
func (s *Server) handleJamSongsPage(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		http.Error(w, "this jam link is invalid or has expired", http.StatusNotFound)
		return
	}
	profile := s.jamProfileFromRequest(r, ev)
	if profile == nil {
		http.Redirect(w, r, "/jam/"+code, http.StatusFound)
		return
	}
	songs, err := s.store.ListJamSongs(r.Context(), code)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// jamSongView adds parsed sections + their section_annotations
	// ("solo: sax") to each picker row, for the inline per-section
	// rendering this phase's brief asks for — lightweight notes, not a
	// body edit, shown alongside the section they're tagged against (see
	// httpapi.buildSections' Index, which section_annotations.section_index
	// lines up with).
	type sectionWithAnnotations struct {
		viewerSection
		Annotations []store.SectionAnnotation
	}
	type jamSongView struct {
		store.JamSong
		Sections []sectionWithAnnotations
	}
	var views []jamSongView
	for _, js := range songs {
		v := jamSongView{JamSong: js}
		if sg, _, err := s.store.LoadVariant(js.ID); err == nil {
			notes, _ := s.store.ListAllSectionAnnotations(r.Context(), js.ID, nil, &profile.ID)
			byIndex := map[int][]store.SectionAnnotation{}
			for _, n := range notes {
				byIndex[n.SectionIndex] = append(byIndex[n.SectionIndex], n)
			}
			for _, sec := range buildSections(sg) {
				v.Sections = append(v.Sections, sectionWithAnnotations{viewerSection: sec, Annotations: byIndex[sec.Index]})
			}
		}
		views = append(views, v)
	}

	s.render(w, "jam_songs", map[string]any{
		"Code": code, "Name": ev.Name, "Nickname": profile.Nickname, "Songs": views,
		"CommonInstruments": commonInstruments,
	})
}

// handleJamPick toggles (POST sets, DELETE removes) the current jam
// profile's pick for one song+instrument.
func (s *Server) handleJamPick(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		httpError(w, http.StatusNotFound, "this jam link is invalid or has expired")
		return
	}
	profile := s.jamProfileFromRequest(r, ev)
	if profile == nil {
		httpError(w, http.StatusUnauthorized, "join this jam first")
		return
	}
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	songID, err := strconv.ParseInt(r.FormValue("song_id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song id")
		return
	}
	instrument := strings.TrimSpace(r.FormValue("instrument"))
	if instrument == "" {
		httpError(w, http.StatusBadRequest, "instrument is required")
		return
	}

	if r.Method == http.MethodDelete {
		err = s.store.RemoveJamPick(r.Context(), ev.ID, profile.ID, songID, instrument)
	} else {
		err = s.store.SetJamPick(r.Context(), ev.ID, profile.ID, songID, instrument)
	}
	if errors.Is(err, store.ErrInstrumentFull) {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleReorderJamSongs sets the jam's pickable-song play order — the
// organizer's up/down reorder controls on jam_roster.html post the whole
// current order, same "send the whole list" pattern as
// handleSaveOrder/store.SaveOrder for a book's own song order. Gated by
// requireJamOrganizer: organizer-for-this-event OR project editor/owner.
func (s *Server) handleReorderJamSongs(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		httpError(w, http.StatusNotFound, "this jam link is invalid or has expired")
		return
	}
	var req struct {
		SongIDs []int64 `json:"song_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.ReorderJamSongs(r.Context(), ev.ID, req.SongIDs); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleCallJamParticipant sets ("call") or clears the organizer's
// "now calling" state for the jam — the prominent roster banner (e.g.
// "Now calling: Alex on bass for 'Mercy'"). Gated by requireJamOrganizer.
func (s *Server) handleCallJamParticipant(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		httpError(w, http.StatusNotFound, "this jam link is invalid or has expired")
		return
	}
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	if r.FormValue("clear") == "1" {
		if err := s.store.ClearJamCall(r.Context(), ev.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		http.Redirect(w, r, "/songbooks/"+strconv.FormatInt(ev.SongbookID, 10)+"/jam/"+code+"/roster", http.StatusFound)
		return
	}
	profileID, err := strconv.ParseInt(r.FormValue("jam_profile_id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid jam_profile_id")
		return
	}
	songID, err := strconv.ParseInt(r.FormValue("song_id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song_id")
		return
	}
	instrument := strings.TrimSpace(r.FormValue("instrument"))
	if err := s.store.SetJamCall(r.Context(), ev.ID, profileID, songID, instrument); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/songbooks/"+strconv.FormatInt(ev.SongbookID, 10)+"/jam/"+code+"/roster", http.StatusFound)
}

// handleJamCallStatus is the small JSON endpoint the roster page polls
// (plain setInterval fetch — "simple polling refresh is fine, no
// websockets") to keep the "now calling" banner live without a full page
// reload.
func (s *Server) handleJamCallStatus(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		httpError(w, http.StatusNotFound, "this jam link is invalid or has expired")
		return
	}
	p := personFromContext(r)
	isOrganizer, err := s.store.IsJamOrganizer(r.Context(), ev.ID, p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasAccess, err := s.store.CanAccessSongbook(r.Context(), p.ID, ev.SongbookID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !isOrganizer && !hasAccess {
		httpError(w, http.StatusForbidden, "forbidden")
		return
	}
	cs, err := s.store.GetJamCallStatus(r.Context(), ev.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"called": cs})
}

// handleAddJamOrganizer lets a project editor/owner designate another
// person (by username) an organizer for this specific jam event — a
// project-level decision, so gated by requireRole("editor", ...) in
// router.go, not requireJamOrganizer.
func (s *Server) handleAddJamOrganizer(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		httpError(w, http.StatusNotFound, "this jam link is invalid or has expired")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if username == "" {
		httpError(w, http.StatusBadRequest, "username is required")
		return
	}
	person, err := s.store.GetPersonByUsername(r.Context(), username)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such person: "+username)
		return
	}
	if err := s.store.AddJamOrganizer(r.Context(), ev.ID, person.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/songbooks/"+strconv.FormatInt(ev.SongbookID, 10)+"/jam/"+code+"/roster", http.StatusFound)
}

// handleJamRosterPage is the organiser's view: who picked what, on which
// instrument, for every song. Gated by a normal logged-in session with at
// least viewer role on the project — not the jam cookie.
//
// "Ready" here is deliberately the simplest possible v1 reading: a song
// with at least one pick on ANY instrument. internal/song doesn't model
// "instruments this song needs" anywhere yet, so there's no real
// completeness check to render beyond "has anyone signed up for this at
// all" — that's a reasonable gap to leave for later, not something to
// invent new schema for in this pass.
func (s *Server) handleJamRosterPage(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		http.Error(w, "this jam link is invalid or has expired", http.StatusNotFound)
		return
	}

	// Viewing the roster needs songbook access OR designated
	// jam-organizer status — a jam organizer need not have any access
	// to the songbook at all (see requireJamOrganizer's doc comment).
	p := personFromContext(r)
	isOrganizer, err := s.store.IsJamOrganizer(r.Context(), ev.ID, p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasAccess, err := s.store.CanAccessSongbook(r.Context(), p.ID, ev.SongbookID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !isOrganizer && !hasAccess {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	roster, err := s.store.JamRoster(r.Context(), ev.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type songGroup struct {
		SongID    int64
		Title     string
		PlayOrder int
		Rows      []store.RosterRow
	}
	var groups []songGroup
	for _, row := range roster {
		if len(groups) == 0 || groups[len(groups)-1].SongID != row.SongID {
			groups = append(groups, songGroup{SongID: row.SongID, Title: row.SongTitle, PlayOrder: row.PlayOrder})
		}
		g := &groups[len(groups)-1]
		g.Rows = append(g.Rows, row)
	}

	// CanOrganize drives whether the reorder/call/add-organizer controls
	// render at all — those endpoints re-check this server-side too
	// (requireJamOrganizer/requireRole), this is purely "don't show
	// buttons that would just 403." isOrganizer/role/isMember were already
	// resolved above for the view-access check.
	canEdit, err := s.store.CanAccessSongbook(r.Context(), p.ID, ev.SongbookID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	canOrganize := isOrganizer || canEdit
	organizers, err := s.store.ListJamOrganizers(r.Context(), ev.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	called, err := s.store.GetJamCallStatus(r.Context(), ev.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.render(w, "jam_roster", map[string]any{
		"SongbookID": ev.SongbookID, "Code": code, "Name": ev.Name,
		"JamURL": "/jam/" + code, "Groups": groups,
		"CanOrganize": canOrganize, "Organizers": organizers, "Called": called,
	})
}

// handleJamOrganizerLinkPage shows the "become an organizer" link/QR code
// on its own page, separate from the participant roster page, specifically
// so an organizer never shares the wrong one by mistake (the whole reason
// this is its own route rather than a section on jam_roster.html).
func (s *Server) handleJamOrganizerLinkPage(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	ev, err := s.store.GetJamEvent(r.Context(), code)
	if err != nil {
		http.Error(w, "this jam link is invalid or has expired", http.StatusNotFound)
		return
	}
	p := personFromContext(r)
	isOrganizer, err := s.store.IsJamOrganizer(r.Context(), ev.ID, p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	canEdit, err := s.store.CanAccessSongbook(r.Context(), p.ID, ev.SongbookID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !isOrganizer && !canEdit {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.render(w, "jam_organizer_link", map[string]any{
		"SongbookID": ev.SongbookID, "Code": code, "Name": ev.Name,
		"OrganizerURL": "/jam-organize/" + ev.OrganizerToken,
	})
}

// --- Jam participants adding to the pickable list themselves (this
// phase's extension of jam mode: "a jam session participant can add any
// song from the global catalog and even create a new song/variant for
// the jam"). All three handlers below are gated by requireJamProfile —
// any signed-up participant, not just the organizer/editor-gated
// handleJamAddSongsPage/handleAddJamSong above, which remain unchanged
// and keep listing only the PARENT PROJECT's own songs (a deliberately
// narrower, organizer-curated candidate list). Participants instead pick
// from the whole global catalog via store.ListCatalog, same as the main
// /catalog browse page — ListCatalog already excludes visibility='jam'
// rows (see catalog.go), so a participant can never pick another jam's
// jam-scoped variant this way, only global catalog material (plus, once
// this event's own fresh/forked jam variants exist, those still show up
// via ListJamSongs regardless, since that query isn't visibility-filtered
// the way ListCatalog is).

// handleJamCatalogPage is the participant's "add a song from the
// catalog" page — the whole global catalog (viewerID 0: a jam
// participant has no person id, so only visibility='global' rows match
// ListCatalog's filter, never anyone's private 'user' rows), filtered by
// the same artist/project query params the main catalog page uses.
func (s *Server) handleJamCatalogPage(w http.ResponseWriter, r *http.Request) {
	ev, profile := jamEventFromContext(r), jamProfileFromContext(r)
	code := r.PathValue("code")
	filter := store.CatalogFilter{
		Artist: r.URL.Query().Get("artist"),
		Sort:   r.URL.Query().Get("sort"),
	}
	variants, err := s.store.ListCatalog(r.Context(), 0, filter)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "jam_catalog_add", map[string]any{
		"Code": code, "Name": ev.Name, "Nickname": profile.Nickname,
		"Variants": variants, "Filter": filter,
	})
}

// handleJamAddCatalogSong adds an existing (global-catalog) variant to
// the jam's pickable list, as the signed-up participant themselves —
// same store.AddJamSong call and same store.DefaultInstrumentLimits()
// starting caps the organizer-facing handleAddJamSong uses, just reached
// by any participant instead of requiring editor role. Re-adding an
// already-listed song is a harmless no-op (see AddJamSong's doc comment).
func (s *Server) handleJamAddCatalogSong(w http.ResponseWriter, r *http.Request) {
	ev := jamEventFromContext(r)
	code := r.PathValue("code")
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	variantID, err := strconv.ParseInt(r.FormValue("variant_id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	if err := s.store.AddJamSong(r.Context(), ev.ID, variantID, store.DefaultInstrumentLimits()); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/jam/"+code+"/songs", http.StatusFound)
}

// handleJamNewVariantPage is the participant's "write a brand-new song
// for tonight" form — title/artist/body, same shape as a normal new-song
// creation, just destined for CreateJamVariant instead of store.CreateSong.
func (s *Server) handleJamNewVariantPage(w http.ResponseWriter, r *http.Request) {
	ev, profile := jamEventFromContext(r), jamProfileFromContext(r)
	code := r.PathValue("code")
	s.render(w, "jam_new_variant", map[string]any{
		"Code": code, "Name": ev.Name, "Nickname": profile.Nickname,
	})
}

// handleJamCreateVariant creates a brand-new, jam-scoped (visibility=
// 'jam', owner_jam_event_id = this event) variant from a title/artist
// pair, via store.CreateJamVariant — which also adds it to this event's
// jam_songs list in the same transaction, so it shows up on
// /jam/{code}/songs immediately. creatorPersonID is 0 (unknown/NULL)
// here: a jam_profiles participant need not be a logged-in Person at all
// — see store.CreateJamVariant's doc comment.
func (s *Server) handleJamCreateVariant(w http.ResponseWriter, r *http.Request) {
	ev := jamEventFromContext(r)
	code := r.PathValue("code")
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	artist := strings.TrimSpace(r.FormValue("artist"))
	if _, err := s.store.CreateJamVariant(r.Context(), ev.ID, title, artist, 0); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/jam/"+code+"/songs", http.StatusFound)
}

// handleJamForkVariant forks an existing (global, personal, or band)
// variant into THIS jam's own scope — store.ForkVariant with scope="jam",
// which both creates the jam-scoped copy (lineage preserved via
// forked_from_id) and adds it to jam_songs, same "a jam variant that
// isn't on the song list doesn't make sense" rule as handleJamCreateVariant.
func (s *Server) handleJamForkVariant(w http.ResponseWriter, r *http.Request) {
	ev := jamEventFromContext(r)
	code := r.PathValue("code")
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	variantID, err := strconv.ParseInt(r.FormValue("variant_id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	if _, err := s.store.ForkVariant(r.Context(), variantID, 0, "jam", ev.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/jam/"+code+"/songs", http.StatusFound)
}
