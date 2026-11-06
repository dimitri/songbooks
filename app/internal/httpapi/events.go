package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"app/internal/store"
)

// requireEventManager gates an event-addressed ({id} = event id) route
// to whoever may manage it — an explicitly assigned event manager, or
// anyone who could have created the event in the first place (community
// staff for a community event, any band member for a band event — see
// store.CanManageEvent's own doc comment), or a site admin.
func (s *Server) requireEventManager(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		eventID, err := pathID(r, "id")
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid event id")
			return
		}
		person := personFromContext(r)
		if person.IsAdmin {
			next(w, r)
			return
		}
		ok, err := s.store.CanManageEvent(r.Context(), eventID, person.ID)
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			httpError(w, http.StatusForbidden, "must manage this event")
			return
		}
		next(w, r)
	})
}

type eventRequestBody struct {
	EventType     string `json:"eventType"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Location      string `json:"location"`
	StartsAt      string `json:"startsAt"` // RFC3339
	EndsAt        string `json:"endsAt"`   // RFC3339, optional
	AudienceScope string `json:"audienceScope"`
}

func parseEventTimes(startsAt, endsAt string) (time.Time, *time.Time, error) {
	st, err := time.Parse(time.RFC3339, startsAt)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("invalid startsAt: %w", err)
	}
	if endsAt == "" {
		return st, nil, nil
	}
	et, err := time.Parse(time.RFC3339, endsAt)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("invalid endsAt: %w", err)
	}
	return st, &et, nil
}

var validEventTypes = map[string]bool{
	"jam": true, "concert": true, "meetup": true, "listening_party": true, "dinner": true, "show_together": true,
}

// handleCreateCommunityEvent creates a community-hosted event. Staff-only
// (requireCommunityStaff, which a site admin already bypasses).
func (s *Server) handleCreateCommunityEvent(w http.ResponseWriter, r *http.Request) {
	communityID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid community id")
		return
	}
	var req eventRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !validEventTypes[req.EventType] {
		httpError(w, http.StatusBadRequest, "invalid eventType")
		return
	}
	startsAt, endsAt, err := parseEventTimes(req.StartsAt, req.EndsAt)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	scope := req.AudienceScope
	if scope != "members" && scope != "bands" && scope != "both" {
		scope = "members"
	}
	actor := personFromContext(r)
	id, err := s.store.CreateEvent(r.Context(), "community", communityID, req.EventType, req.Name, req.Description, req.Location, startsAt, endsAt, scope, actor.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"eventId": id})
}

// handleCreateBandEvent creates a band-hosted event (a concert/gig, or
// any other type a band wants to use informally) — never public, see
// canViewEvent. Any-current-member-gated (requireBandMember), same
// flat-membership rule as every other band action.
func (s *Server) handleCreateBandEvent(w http.ResponseWriter, r *http.Request) {
	bandID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid band id")
		return
	}
	var req eventRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !validEventTypes[req.EventType] {
		httpError(w, http.StatusBadRequest, "invalid eventType")
		return
	}
	startsAt, endsAt, err := parseEventTimes(req.StartsAt, req.EndsAt)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	actor := personFromContext(r)
	id, err := s.store.CreateEvent(r.Context(), "band", bandID, req.EventType, req.Name, req.Description, req.Location, startsAt, endsAt, "members", actor.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"eventId": id})
}

// handleCommunityEventsPage is the public (no auth) "upcoming events /
// past events, register here" page for one community — the user's own
// explicit spec: "a public page (everyone) that lists upcoming events…
// and past events, and allows one to register."
func (s *Server) handleCommunityEventsPage(w http.ResponseWriter, r *http.Request) {
	communityID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid community id")
		return
	}
	community, err := s.store.GetCommunity(r.Context(), communityID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events, err := s.store.ListEventsForCommunity(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	var upcoming, past []store.Event
	for _, ev := range events {
		if ev.IsPast(now) {
			past = append(past, ev)
		} else {
			upcoming = append(upcoming, ev)
		}
	}
	s.render(w, "community_events", map[string]any{
		"Me": personFromContext(r), "Community": community, "Upcoming": upcoming, "Past": past,
	})
}

// handleBandEventsPage lists bandID's own events — requireBandMember,
// never public.
func (s *Server) handleBandEventsPage(w http.ResponseWriter, r *http.Request) {
	bandID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid band id")
		return
	}
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events, err := s.store.ListEventsForBand(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	var upcoming, past []store.Event
	for _, ev := range events {
		if ev.IsPast(now) {
			past = append(past, ev)
		} else {
			upcoming = append(upcoming, ev)
		}
	}
	s.render(w, "band_events", map[string]any{
		"Me": personFromContext(r), "Band": band, "Upcoming": upcoming, "Past": past,
	})
}

// eventSongRow is one setlist entry resolved for display on the event
// page, plus whoever has already registered to play it.
type eventSongRow struct {
	VariantID     int64
	Title, Artist string
	Registrations []store.EventSongRegistration
}

// handleEventPage is the single event detail page — public for a
// community-owned event, band-member-gated for a band-owned one (see
// canViewEvent). Shows date/place/description, an add-to-calendar
// link, the attendee list, the attached setlist's songs with whoever's
// registered to play each (if any), and a register form.
func (s *Server) handleEventPage(w http.ResponseWriter, r *http.Request) {
	eventID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	ev, err := s.store.GetEvent(r.Context(), eventID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	person := personFromContext(r)
	if ev.OwnerKind == "band" {
		if person == nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		if !person.IsAdmin {
			isMember, err := s.store.IsCurrentBandMember(r.Context(), ev.OwnerBandID, person.ID)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if !isMember {
				httpError(w, http.StatusForbidden, "must be a current member of this band")
				return
			}
		}
	}

	attendees, err := s.store.ListEventRegistrations(r.Context(), eventID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	alreadyRegistered := false
	if person != nil {
		for _, a := range attendees {
			if a.PersonID == person.ID {
				alreadyRegistered = true
				break
			}
		}
	}

	var songRows []eventSongRow
	if ev.SetlistID != nil {
		songRows, err = s.eventSongRows(r.Context(), eventID, *ev.SetlistID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	var myBands []store.Band
	if person != nil {
		myBands, _ = s.store.ListBandsForPerson(r.Context(), person.ID)
	}

	s.render(w, "event", map[string]any{
		"Me": person, "Event": ev, "Attendees": attendees, "AlreadyRegistered": alreadyRegistered,
		"SongRows": songRows, "MyBands": myBands,
	})
}

// eventSongRows resolves eventID's attached setlist into display rows
// (title/artist + who's registered to play each) — loop-and-resolve
// rather than a batch join, acceptable for a setlist's realistic size
// (a handful to a few dozen songs).
func (s *Server) eventSongRows(ctx context.Context, eventID, setlistID int64) ([]eventSongRow, error) {
	l, _, _, err := s.store.LoadSetlistByID(ctx, setlistID)
	if err != nil {
		return nil, err
	}
	regs, err := s.store.ListEventSongRegistrations(ctx, eventID)
	if err != nil {
		return nil, err
	}
	regsByVariant := map[int64][]store.EventSongRegistration{}
	for _, reg := range regs {
		regsByVariant[reg.VariantID] = append(regsByVariant[reg.VariantID], reg)
	}
	var rows []eventSongRow
	seen := map[int64]bool{}
	for _, set := range l.Sets {
		for _, entry := range set.Entries {
			if !entry.IsVariant() || seen[entry.VariantID] {
				continue
			}
			seen[entry.VariantID] = true
			v, err := s.store.GetVariant(ctx, entry.VariantID)
			if err != nil {
				continue
			}
			rows = append(rows, eventSongRow{
				VariantID: entry.VariantID, Title: v.Title, Artist: v.Artist,
				Registrations: regsByVariant[entry.VariantID],
			})
		}
	}
	return rows, nil
}

// handleEventICS is the add-to-calendar button's target — a minimal
// RFC 5545 VCALENDAR with one VEVENT, no external dependency (this
// codebase already prefers hand-written formats over pulling in a
// library for a small, well-specified text format — same posture as
// internal/setlist's own doc comment).
func (s *Server) handleEventICS(w http.ResponseWriter, r *http.Request) {
	eventID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	ev, err := s.store.GetEvent(r.Context(), eventID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ev.OwnerKind == "band" {
		person := personFromContext(r)
		if person == nil || (!person.IsAdmin && func() bool {
			ok, _ := s.store.IsCurrentBandMember(r.Context(), ev.OwnerBandID, person.ID)
			return !ok
		}()) {
			httpError(w, http.StatusForbidden, "must be a current member of this band")
			return
		}
	}
	ics := buildICS(ev)
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="event-`+strconv.FormatInt(ev.ID, 10)+`.ics"`)
	w.Write([]byte(ics))
}

func icsEscape(s string) string {
	r := strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n")
	return r.Replace(s)
}

func buildICS(ev *store.Event) string {
	fmtTime := func(t time.Time) string { return t.UTC().Format("20060102T150405Z") }
	end := ev.StartsAt.Add(2 * time.Hour)
	if ev.EndsAt != nil {
		end = *ev.EndsAt
	}
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//Songbooks//Events//EN\r\n")
	b.WriteString("BEGIN:VEVENT\r\n")
	b.WriteString("UID:event-" + strconv.FormatInt(ev.ID, 10) + "@songbooks\r\n")
	b.WriteString("DTSTAMP:" + fmtTime(time.Now()) + "\r\n")
	b.WriteString("DTSTART:" + fmtTime(ev.StartsAt) + "\r\n")
	b.WriteString("DTEND:" + fmtTime(end) + "\r\n")
	b.WriteString("SUMMARY:" + icsEscape(ev.Name) + "\r\n")
	if ev.Location != "" {
		b.WriteString("LOCATION:" + icsEscape(ev.Location) + "\r\n")
	}
	if ev.Description != "" {
		b.WriteString("DESCRIPTION:" + icsEscape(ev.Description) + "\r\n")
	}
	b.WriteString("END:VEVENT\r\n")
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

// handleRegisterForEvent is the public (or band-member, for a band
// event) "register for this event" form post — if the visitor is
// already logged in, registers them directly; otherwise creates a real
// account on the spot (the user's own explicit choice: not an
// anonymous jam-style profile) and registers that. Mirrors
// handlePostInvite's two-branch shape.
func (s *Server) handleRegisterForEvent(w http.ResponseWriter, r *http.Request) {
	eventID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	ev, err := s.store.GetEvent(r.Context(), eventID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such event")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := r.ParseForm(); err != nil {
		httpError(w, http.StatusBadRequest, "invalid form")
		return
	}
	var bandID int64
	if v := r.FormValue("band_id"); v != "" {
		bandID, _ = strconv.ParseInt(v, 10, 64)
	}

	// personFromContext, not a manually re-derived session cookie — this
	// route is wrapped in optionalPerson (router.go), which already
	// resolves the logged-in person correctly, dev-auto-login included;
	// re-reading r.Cookie(sessionCookie) here missed that path entirely
	// (dev-auto-login has no real session cookie at all) and silently
	// fell through to the anonymous-visitor branch even for a logged-in
	// user, which then failed validation since the page hides the
	// username/password fields whenever .Me is set.
	if p := personFromContext(r); p != nil {
		if ev.OwnerKind == "band" {
			isMember, _ := s.store.IsCurrentBandMember(r.Context(), ev.OwnerBandID, p.ID)
			if !isMember && !p.IsAdmin {
				httpError(w, http.StatusForbidden, "must be a current member of this band")
				return
			}
		}
		if err := s.store.RegisterForEvent(r.Context(), eventID, p.ID, bandID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		http.Redirect(w, r, "/events/"+strconv.FormatInt(eventID, 10), http.StatusFound)
		return
	}

	if ev.OwnerKind == "band" {
		httpError(w, http.StatusForbidden, "log in as a member of this band to register")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	if username == "" || displayName == "" {
		httpError(w, http.StatusBadRequest, "username and display name are required")
		return
	}
	var hash string
	if password != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("hashing event-registration password: %v", err)
			httpError(w, http.StatusInternalServerError, "couldn't register — try again")
			return
		}
		hash = string(h)
	}
	personID, err := s.store.CreatePersonAndRegisterForEvent(r.Context(), eventID, username, displayName, email, hash, bandID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrUsernameTaken):
			httpError(w, http.StatusConflict, "that username is already taken — try another")
		case errors.Is(err, store.ErrEmailTaken):
			httpError(w, http.StatusConflict, "that email is already registered to another account")
		default:
			log.Printf("registering for event %d: %v", eventID, err)
			httpError(w, http.StatusInternalServerError, "couldn't register — try again")
		}
		return
	}
	sessToken, err := s.store.CreateSession(r.Context(), personID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sessToken, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		MaxAge: 30 * 24 * 60 * 60,
	})
	http.Redirect(w, r, "/events/"+strconv.FormatInt(eventID, 10), http.StatusFound)
}

// handleRegisterSongForEvent claims a song from eventID's attached
// setlist — as the logged-in person, or as a band they're a current
// member of (req.bandId). requireAuth only: any logged-in visitor who
// can see the event can register a song, same "no extra gate beyond
// being able to view it" rule the public registration flow uses.
func (s *Server) handleRegisterSongForEvent(w http.ResponseWriter, r *http.Request) {
	eventID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	var req struct {
		VariantID int64 `json:"variantId"`
		BandID    int64 `json:"bandId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VariantID == 0 {
		httpError(w, http.StatusBadRequest, "variantId is required")
		return
	}
	ev, err := s.store.GetEvent(r.Context(), eventID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such event")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// audience_scope gates WHO may claim a song, not just the UI's own
	// copy: "scène ouverte" (bands) shouldn't quietly accept a solo
	// musician registration, and a members-only jam shouldn't accept a
	// band one.
	if ev.AudienceScope == "bands" && req.BandID == 0 {
		httpError(w, http.StatusBadRequest, "this event is open to bands only — pick a band")
		return
	}
	if ev.AudienceScope == "members" && req.BandID != 0 {
		httpError(w, http.StatusBadRequest, "this event is open to individual musicians only")
		return
	}
	actor := personFromContext(r)
	personID := actor.ID
	bandID := int64(0)
	if req.BandID != 0 {
		isMember, err := s.store.IsCurrentBandMember(r.Context(), req.BandID, actor.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isMember && !actor.IsAdmin {
			httpError(w, http.StatusForbidden, "must be a current member of that band")
			return
		}
		bandID, personID = req.BandID, 0
	}
	id, err := s.store.RegisterSongForEvent(r.Context(), eventID, req.VariantID, bandID, personID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"registrationId": id})
}

// handleRemoveSongRegistration withdraws a song registration — the
// registrant themselves, or anyone who manages the event.
func (s *Server) handleRemoveSongRegistration(w http.ResponseWriter, r *http.Request) {
	eventID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	regID, err := pathID(r, "regID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid registration id")
		return
	}
	actor := personFromContext(r)
	if !actor.IsAdmin {
		canManage, err := s.store.CanManageEvent(r.Context(), eventID, actor.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !canManage {
			// Not a manager — allow only if this registration is their own.
			regs, err := s.store.ListEventSongRegistrations(r.Context(), eventID)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			owns := false
			for _, reg := range regs {
				if reg.ID == regID && reg.PersonID == actor.ID {
					owns = true
					break
				}
			}
			if !owns {
				httpError(w, http.StatusForbidden, "not your registration")
				return
			}
		}
	}
	if err := s.store.RemoveSongRegistration(r.Context(), regID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// handleEventManagePage is the assigned event manager's own page:
// attach/create the event's setlist, see who's registered to play
// what. "Event managers manage the songbook and the setlist basically"
// — the user's own framing.
func (s *Server) handleEventManagePage(w http.ResponseWriter, r *http.Request) {
	eventID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	ev, err := s.store.GetEvent(r.Context(), eventID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var songRows []eventSongRow
	if ev.SetlistID != nil {
		songRows, err = s.eventSongRows(r.Context(), eventID, *ev.SetlistID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	managers, err := s.store.ListEventManagers(r.Context(), eventID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The event owner's existing songbooks, for "attach an existing one"
	// instead of always creating a brand-new dedicated one.
	ownerKind, ownerID := "band", ev.OwnerBandID
	if ev.OwnerKind == "community" {
		ownerKind, ownerID = "community", ev.OwnerCommunityID
	}
	ownerSongbooks, err := s.store.ListSongbooks(r.Context(), ownerKind, ownerID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "event_manage", map[string]any{
		"Me": personFromContext(r), "Event": ev, "SongRows": songRows, "Managers": managers,
		"OwnerSongbooks": ownerSongbooks,
	})
}

// handleCreateEventSetlist creates a fresh setlist (scoped to the
// event's own owner — community or band) and attaches it to eventID, for
// an event that doesn't have one yet.
func (s *Server) handleCreateEventSetlist(w http.ResponseWriter, r *http.Request) {
	eventID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	ev, err := s.store.GetEvent(r.Context(), eventID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such event")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	actor := personFromContext(r)
	var ownerBandID, ownerCommunityID *int64
	scope := "band"
	if ev.OwnerKind == "community" {
		scope = "community"
		id := ev.OwnerCommunityID
		ownerCommunityID = &id
	} else {
		id := ev.OwnerBandID
		ownerBandID = &id
	}
	setlistID, err := s.store.CreateSetlist(r.Context(), scope, ev.Name, "", nil, ownerBandID, ownerCommunityID, actor.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.SetEventSetlist(r.Context(), eventID, setlistID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"setlistId": setlistID})
}

// handleCreateEventSongbook creates a brand-new songbook owned by the
// event's own community/band and attaches it as the event's dedicated
// songbook (SetEventSongbook) — the pool of material being prepped for
// it, distinct from and alongside its setlist (handleCreateEventSetlist).
func (s *Server) handleCreateEventSongbook(w http.ResponseWriter, r *http.Request) {
	eventID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	ev, err := s.store.GetEvent(r.Context(), eventID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such event")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ownerKind, ownerID := "band", ev.OwnerBandID
	if ev.OwnerKind == "community" {
		ownerKind, ownerID = "community", ev.OwnerCommunityID
	}
	songbookID, err := s.store.CreateSongbook(r.Context(), ownerKind, ownerID, ev.Name, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.SetEventSongbook(r.Context(), eventID, songbookID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"songbookId": songbookID})
}

// handleAttachEventSongbook attaches an EXISTING community/band songbook
// (one the event's owner already has) as the event's dedicated songbook,
// instead of creating a new one.
func (s *Server) handleAttachEventSongbook(w http.ResponseWriter, r *http.Request) {
	eventID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	var req struct {
		SongbookID int64 `json:"songbookId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SongbookID == 0 {
		httpError(w, http.StatusBadRequest, "songbookId is required")
		return
	}
	if err := s.store.SetEventSongbook(r.Context(), eventID, req.SongbookID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleAssignEventManager and handleRemoveEventManagerFor* share one
// implementation, scoped by the owner-level middleware that calls
// them (requireCommunityStaff / requireBandMember) — "assigned by the jam
// staff / community staff," the user's own explicit spec: assignment
// itself stays at the owner-staff level, not delegable to an existing
// event manager.
func (s *Server) handleAssignEventManager(w http.ResponseWriter, r *http.Request) {
	eventID, err := pathID(r, "eventID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	var req struct {
		PersonID int64 `json:"personId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PersonID == 0 {
		httpError(w, http.StatusBadRequest, "personId is required")
		return
	}
	actor := personFromContext(r)
	if err := s.store.AddEventManager(r.Context(), eventID, req.PersonID, actor.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRemoveEventManager(w http.ResponseWriter, r *http.Request) {
	eventID, err := pathID(r, "eventID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	personID, err := pathID(r, "personID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	if err := s.store.RemoveEventManager(r.Context(), eventID, personID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
