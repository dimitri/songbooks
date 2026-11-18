package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"app/internal/chordsym"
	"app/internal/store"
)

// A coaching community's repertoire and its events' programs (docs/
// coaching.md): the coach sees every song their students and groups
// have, each with who has it and in which key; picks songs for an open
// mic or the end-of-year concert; and plans each one: who sings lead,
// who sings backing vocals, in which key (the lead's, by default). The
// songs then open in that key from the program, for everyone in it.

type repertoireRow struct {
	store.RepertoireSong
	Singers []repertoireSingerView
	Search  string // what the filter matches: title, artist, singers
}

type repertoireSingerView struct {
	store.RepertoireSinger
	KeyName string
}

// handleCoachRepertoirePage is GET /communities/{id}/student-songs.
func (s *Server) handleCoachRepertoirePage(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		http.Redirect(w, r, fmt.Sprintf("/communities/%d/students/%d", community.ID, personFromContext(r).ID), http.StatusFound)
		return
	}
	songs, err := s.store.CommunityRepertoire(r.Context(), community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows := make([]repertoireRow, 0, len(songs))
	for _, sg := range songs {
		if sg.Key == "" {
			sg.Key = s.songKeyOf(r.Context(), sg.VariantID)
		}
		row := repertoireRow{RepertoireSong: sg}
		words := []string{sg.Title, sg.Artist}
		for _, g := range sg.Singers {
			row.Singers = append(row.Singers, repertoireSingerView{RepertoireSinger: g, KeyName: chordsym.KeyIn(sg.Key, g.Semitones)})
			words = append(words, g.Name)
		}
		row.Search = strings.ToLower(strings.Join(words, " "))
		rows = append(rows, row)
	}
	events, err := s.programEvents(r, community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := s.coachingPageData(r, community, coach, "repertoire")
	data["Songs"] = rows
	data["Events"] = events
	s.render(w, "coaching_repertoire", data)
}

type programEvent struct {
	store.Event
	SongCount int
}

// programEvents are a community's upcoming events a program can be
// planned for (not its lessons), soonest first, with their songs counted.
func (s *Server) programEvents(r *http.Request, communityID int64) ([]programEvent, error) {
	all, err := s.store.ListEventsForCommunity(r.Context(), communityID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []programEvent
	for _, ev := range all {
		if ev.EventType == "lesson" || ev.IsPast(now) {
			continue
		}
		p, err := s.store.ListEventProgram(r.Context(), ev.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, programEvent{Event: ev, SongCount: len(p)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartsAt.Before(out[j].StartsAt) })
	return out, nil
}

// handlePostProgramPlan is POST /api/communities/{id}/program-plan: the
// songs picked on the repertoire page added to an event's program, an
// existing one (eventId) or a new one (name, type, date and time).
func (s *Server) handlePostProgramPlan(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return
	}
	var req struct {
		EventID    int64   `json:"eventId"`
		Name       string  `json:"name"`
		EventType  string  `json:"eventType"`
		StartsAt   string  `json:"startsAt"` // 2006-01-02T15:04, local
		Location   string  `json:"location"`
		VariantIDs []int64 `json:"variantIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	eventID := req.EventID
	if eventID != 0 {
		if _, ok := s.communityEvent(w, r, community.ID, eventID); !ok {
			return
		}
	} else {
		name := strings.TrimSpace(req.Name)
		if name == "" {
			httpError(w, http.StatusBadRequest, "name is required")
			return
		}
		if req.EventType != "open_mic" && req.EventType != "concert" && req.EventType != "masterclass" {
			req.EventType = "concert"
		}
		start, err := time.ParseInLocation("2006-01-02T15:04", req.StartsAt, time.Local)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid start time")
			return
		}
		if eventID, err = s.store.CreateEvent(r.Context(), "community", community.ID, req.EventType, name, "", strings.TrimSpace(req.Location), start, nil, "members", personFromContext(r).ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.store.AddProgramSongs(r.Context(), eventID, req.VariantIDs); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.seedProgramLeads(r, community.ID, eventID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"eventId": eventID, "url": fmt.Sprintf("/communities/%d/events/%d/program", community.ID, eventID)})
}

// seedProgramLeads gives each program song with no lead yet the one
// student who has it, if only one does, and their key.
func (s *Server) seedProgramLeads(r *http.Request, communityID, eventID int64) error {
	program, err := s.store.ListEventProgram(r.Context(), eventID)
	if err != nil {
		return err
	}
	repertoire, err := s.store.CommunityRepertoire(r.Context(), communityID)
	if err != nil {
		return err
	}
	only := map[int64]store.RepertoireSinger{}
	for _, sg := range repertoire {
		var students []store.RepertoireSinger
		for _, g := range sg.Singers {
			if g.Kind == "student" {
				students = append(students, g)
			}
		}
		if len(students) == 1 {
			only[sg.VariantID] = students[0]
		}
	}
	for _, p := range program {
		g, ok := only[p.VariantID]
		if p.Lead != nil || len(p.Backing) > 0 || !ok {
			continue
		}
		if err := s.store.SetProgramLead(r.Context(), p.ID, g.PersonID); err != nil {
			return err
		}
		if err := s.store.SetProgramKey(r.Context(), eventID, p.ID, g.Semitones); err != nil {
			return err
		}
	}
	return nil
}

// communityEvent is one of the community's events, or a 404.
func (s *Server) communityEvent(w http.ResponseWriter, r *http.Request, communityID, eventID int64) (*store.Event, bool) {
	ev, err := s.store.GetEvent(r.Context(), eventID)
	if err != nil || ev.OwnerKind != "community" || ev.OwnerCommunityID != communityID || ev.EventType == "lesson" {
		http.NotFound(w, r)
		return nil, false
	}
	return ev, true
}

type programSongView struct {
	store.ProgramSong
	KeyName string
	// The students and groups who have the song, in their keys, and the
	// community's students to pick the lead and backing vocals from,
	// those who have it first.
	Has        []repertoireSingerView
	Candidates []programCandidate
	Mine       string // "lead" or "backing" for the person viewing
}

type programCandidate struct {
	PersonID int64
	Name     string
	HasIt    bool
	KeyName  string // their key for it, when they have it
	Lead     bool
	Backing  bool
}

type programSingerTotal struct {
	Name    string
	Lead    int
	Backing int
}

// handleEventProgramPage is GET /communities/{id}/events/{eventID}/program.
func (s *Server) handleEventProgramPage(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	eventID, err := pathID(r, "eventID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ev, ok := s.communityEvent(w, r, community.ID, eventID)
	if !ok {
		return
	}
	program, err := s.store.ListEventProgram(r.Context(), ev.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	repertoire, err := s.store.CommunityRepertoire(r.Context(), community.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	has := map[int64][]store.RepertoireSinger{}
	for _, sg := range repertoire {
		has[sg.VariantID] = sg.Singers
	}
	members, err := s.store.ListCommunityMembers(r.Context(), community.ID, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var students []store.CommunityMember
	for _, m := range members {
		if !m.IsStaff() {
			students = append(students, m)
		}
	}
	sort.SliceStable(students, func(i, j int) bool {
		return store.FoldName(students[i].DisplayName) < store.FoldName(students[j].DisplayName)
	})
	me := personFromContext(r).ID
	totals := map[int64]*programSingerTotal{}
	var views []programSongView
	for _, p := range program {
		if p.Key == "" {
			p.Key = s.songKeyOf(r.Context(), p.VariantID)
		}
		v := programSongView{ProgramSong: p, KeyName: chordsym.KeyIn(p.Key, p.Semitones)}
		studentKey := map[int64]int{}
		for _, g := range has[p.VariantID] {
			v.Has = append(v.Has, repertoireSingerView{RepertoireSinger: g, KeyName: chordsym.KeyIn(p.Key, g.Semitones)})
			if g.Kind == "student" {
				studentKey[g.PersonID] = g.Semitones
			}
		}
		backing := map[int64]bool{}
		for _, b := range p.Backing {
			backing[b.PersonID] = true
			if b.PersonID == me {
				v.Mine = "backing"
			}
		}
		if p.Lead != nil && p.Lead.PersonID == me {
			v.Mine = "lead"
		}
		for _, m := range students {
			c := programCandidate{PersonID: m.PersonID, Name: m.DisplayName, Backing: backing[m.PersonID], Lead: p.Lead != nil && p.Lead.PersonID == m.PersonID}
			if n, ok := studentKey[m.PersonID]; ok {
				c.HasIt, c.KeyName = true, chordsym.KeyIn(p.Key, n)
			}
			v.Candidates = append(v.Candidates, c)
		}
		sort.SliceStable(v.Candidates, func(i, j int) bool { return v.Candidates[i].HasIt && !v.Candidates[j].HasIt })
		count := func(s store.ProgramSinger) *programSingerTotal {
			t := totals[s.PersonID]
			if t == nil {
				t = &programSingerTotal{Name: s.Name}
				totals[s.PersonID] = t
			}
			return t
		}
		if p.Lead != nil {
			count(*p.Lead).Lead++
		}
		for _, b := range p.Backing {
			count(b).Backing++
		}
		views = append(views, v)
	}
	var tally []programSingerTotal
	for _, t := range totals {
		tally = append(tally, *t)
	}
	sort.Slice(tally, func(i, j int) bool { return store.FoldName(tally[i].Name) < store.FoldName(tally[j].Name) })
	var addable []store.RepertoireSong
	in := map[int64]bool{}
	for _, p := range program {
		in[p.VariantID] = true
	}
	for _, sg := range repertoire {
		if !in[sg.VariantID] {
			addable = append(addable, sg)
		}
	}
	data := s.coachingPageData(r, community, coach, "repertoire")
	data["Event"] = ev
	data["Program"] = views
	data["Tally"] = tally
	data["AddSongs"] = addable
	s.render(w, "coaching_program", data)
}

// programSongRequest is what the program's API takes, field by field.
type programSongRequest struct {
	Semitones  *int    `json:"semitones"`
	PersonID   int64   `json:"personId"`
	On         bool    `json:"on"`
	Delta      int     `json:"delta"`
	VariantIDs []int64 `json:"variantIds"`
}

// programContext checks a program API call: a coach, an event of the
// community, and (with {programID}) one of its songs.
func (s *Server) programContext(w http.ResponseWriter, r *http.Request) (community *store.Community, ev *store.Event, programID, variantID int64, req programSongRequest, ok bool) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	ok = false
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return
	}
	eventID, err := pathID(r, "eventID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ev, ok = s.communityEvent(w, r, community.ID, eventID); !ok {
		return
	}
	ok = false
	if r.PathValue("programID") != "" {
		if programID, err = pathID(r, "programID"); err != nil {
			http.NotFound(w, r)
			return
		}
		if variantID, err = s.store.ProgramSongOf(r.Context(), ev.ID, programID); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	if r.Body != nil && r.Method != http.MethodDelete {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	return community, ev, programID, variantID, req, true
}

// programKeyReply is a program song's key after a change.
func (s *Server) programKeyReply(w http.ResponseWriter, r *http.Request, variantID int64, semitones int) {
	writeJSON(w, http.StatusOK, map[string]any{"semitones": semitones, "keyName": chordsym.KeyIn(s.songKeyOf(r.Context(), variantID), semitones)})
}

// handlePostProgramSongs is POST /api/communities/{id}/events/{eventID}/program.
func (s *Server) handlePostProgramSongs(w http.ResponseWriter, r *http.Request) {
	community, ev, _, _, req, ok := s.programContext(w, r)
	if !ok {
		return
	}
	if err := s.store.AddProgramSongs(r.Context(), ev.ID, req.VariantIDs); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.seedProgramLeads(r, community.ID, ev.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePutProgramKey is PUT .../program/{programID}/key.
func (s *Server) handlePutProgramKey(w http.ResponseWriter, r *http.Request) {
	_, ev, programID, variantID, req, ok := s.programContext(w, r)
	if !ok {
		return
	}
	if req.Semitones == nil {
		httpError(w, http.StatusBadRequest, "semitones is required")
		return
	}
	n := max(-12, min(12, *req.Semitones))
	if err := s.store.SetProgramKey(r.Context(), ev.ID, programID, n); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.programKeyReply(w, r, variantID, n)
}

// handlePutProgramLead is PUT .../program/{programID}/lead: the lead
// singer, and the song in their key when they have it.
func (s *Server) handlePutProgramLead(w http.ResponseWriter, r *http.Request) {
	community, ev, programID, variantID, req, ok := s.programContext(w, r)
	if !ok {
		return
	}
	if err := s.store.SetProgramLead(r.Context(), programID, req.PersonID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _, err := s.store.ProgramKeyOf(r.Context(), ev.ID, variantID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.PersonID != 0 {
		if sb, _ := s.store.FindStudentSongbook(r.Context(), community.ID, req.PersonID); sb != 0 {
			if k, err := s.store.EntrySemitones(r.Context(), sb, variantID); err == nil {
				n = k
				if err := s.store.SetProgramKey(r.Context(), ev.ID, programID, n); err != nil {
					httpError(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
		}
	}
	s.programKeyReply(w, r, variantID, n)
}

// handlePutProgramBacking is PUT .../program/{programID}/backing.
func (s *Server) handlePutProgramBacking(w http.ResponseWriter, r *http.Request) {
	_, _, programID, _, req, ok := s.programContext(w, r)
	if !ok {
		return
	}
	if req.PersonID == 0 {
		httpError(w, http.StatusBadRequest, "personId is required")
		return
	}
	if err := s.store.SetProgramBacking(r.Context(), programID, req.PersonID, req.On); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePostProgramMove is POST .../program/{programID}/move.
func (s *Server) handlePostProgramMove(w http.ResponseWriter, r *http.Request) {
	_, ev, programID, _, req, ok := s.programContext(w, r)
	if !ok {
		return
	}
	if req.Delta != -1 && req.Delta != 1 {
		httpError(w, http.StatusBadRequest, "delta is -1 or 1")
		return
	}
	if err := s.store.MoveProgramSong(r.Context(), ev.ID, programID, req.Delta); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteProgramSong is DELETE .../program/{programID}.
func (s *Server) handleDeleteProgramSong(w http.ResponseWriter, r *http.Request) {
	_, ev, programID, _, _, ok := s.programContext(w, r)
	if !ok {
		return
	}
	if err := s.store.RemoveProgramSong(r.Context(), ev.ID, programID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
