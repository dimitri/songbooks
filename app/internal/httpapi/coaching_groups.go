package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"app/internal/store"
)

// A coaching community's groups (docs/coaching.md): a year's class, or a
// one-off around a master class or a stage. Each has its members, its
// songs in its key, and its lessons.

// handleCoachingGroupsPage is GET /communities/{id}/groups: the coach's
// groups; a student's own.
func (s *Server) handleCoachingGroupsPage(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	person := personFromContext(r)
	only := person.ID
	if coach {
		only = 0
	}
	groups, err := s.store.ListCoachingGroups(r.Context(), community.ID, only)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := s.coachingPageData(r, community, coach, "groups")
	data["Groups"] = groups
	if coach {
		students, err := s.communityStudents(r, community.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data["Students"] = students
		// The community's master classes and stages, for a one-off group.
		events, err := s.store.ListEventsForCommunity(r.Context(), community.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var mc []store.Event
		for _, ev := range events {
			if ev.EventType == "masterclass" && !ev.IsPast(time.Now()) {
				mc = append(mc, ev)
			}
		}
		data["Masterclasses"] = mc
		y := time.Now().Year()
		if time.Now().Month() < time.July {
			y--
		}
		data["SeasonStart"] = time.Date(y, time.September, 1, 0, 0, 0, 0, time.Local).Format("2006-01-02")
		data["SeasonEnd"] = time.Date(y+1, time.June, 30, 0, 0, 0, 0, time.Local).Format("2006-01-02")
	}
	s.render(w, "coaching_groups", data)
}

// communityStudents is a community's current members who aren't staff.
func (s *Server) communityStudents(r *http.Request, communityID int64) ([]store.CommunityMember, error) {
	members, err := s.store.ListCommunityMembers(r.Context(), communityID, false)
	if err != nil {
		return nil, err
	}
	var out []store.CommunityMember
	for _, m := range members {
		if !m.IsStaff() {
			out = append(out, m)
		}
	}
	return out, nil
}

// coachingGroup loads {groupID} of a coaching community, for its coach or
// (read) one of its members.
func (s *Server) coachingGroup(w http.ResponseWriter, r *http.Request, write bool) (*store.Community, *store.CoachingGroup, bool, bool) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return nil, nil, false, false
	}
	groupID, err := pathID(r, "groupID")
	if err != nil {
		http.NotFound(w, r)
		return nil, nil, false, false
	}
	g, err := s.store.GetCoachingGroup(r.Context(), groupID)
	if err != nil || g.CommunityID != community.ID || (!coach && !g.HasMember(personFromContext(r).ID)) {
		http.NotFound(w, r)
		return nil, nil, false, false
	}
	if write && !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return nil, nil, false, false
	}
	return community, g, coach, true
}

// handleCoachingGroupPage is GET /communities/{id}/groups/{groupID}.
func (s *Server) handleCoachingGroupPage(w http.ResponseWriter, r *http.Request) {
	community, g, coach, ok := s.coachingGroup(w, r, false)
	if !ok {
		return
	}
	ctx := r.Context()
	songs, err := s.store.ListStudentSongs(ctx, g.SongbookID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	all, err := s.store.ListCoachingLessons(ctx, community.ID, time.Now().Add(-6*time.Hour), 0)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var lessons []store.CoachingLesson
	for _, l := range all {
		if l.Event.CoachingGroupID != nil && *l.Event.CoachingGroupID == g.ID {
			lessons = append(lessons, l)
		}
	}
	data := s.coachingPageData(r, community, coach, "groups")
	data["Group"] = g
	data["Songs"] = songs
	data["SongbookID"] = g.SongbookID
	data["Lessons"] = lessons
	if g.EventID != nil {
		if ev, err := s.store.GetEvent(ctx, *g.EventID); err == nil {
			data["Event"] = ev
		}
	}
	if coach {
		students, err := s.communityStudents(r, community.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data["Students"] = students
		if data["AddSongs"], err = s.songsToAdd(r, community.ID, songs); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.render(w, "coaching_group", data)
}

type coachingGroupRequest struct {
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	StartsOn string  `json:"startsOn"` // 2006-01-02, "" for none
	EndsOn   string  `json:"endsOn"`
	EventID  int64   `json:"eventId"`
	Members  []int64 `json:"members"`
}

func parseDay(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// handlePostCoachingGroup is POST /api/communities/{id}/groups.
func (s *Server) handlePostCoachingGroup(w http.ResponseWriter, r *http.Request) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return
	}
	if !coach {
		httpError(w, http.StatusForbidden, "only the coach can do that")
		return
	}
	var req coachingGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		httpError(w, http.StatusBadRequest, "a group needs a name")
		return
	}
	starts, err1 := parseDay(req.StartsOn)
	ends, err2 := parseDay(req.EndsOn)
	if err1 != nil || err2 != nil {
		httpError(w, http.StatusBadRequest, "invalid dates")
		return
	}
	var eventID *int64
	if req.EventID != 0 {
		ev, err := s.store.GetEvent(r.Context(), req.EventID)
		if err != nil || ev.OwnerCommunityID != community.ID {
			httpError(w, http.StatusBadRequest, "no such event")
			return
		}
		eventID = &req.EventID
		if starts == nil {
			d := ev.StartsAt
			starts, ends = &d, ev.EndsAt
		}
	}
	id, err := s.store.CreateCoachingGroup(r.Context(), community.ID, req.Name, req.Kind, starts, ends, eventID)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.setGroupMembers(r, community.ID, id, req.Members); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// setGroupMembers makes students the group's members (current students
// of the community only); a one-off group's event gets them registered.
func (s *Server) setGroupMembers(r *http.Request, communityID, groupID int64, members []int64) error {
	for _, id := range members {
		if _, ok := s.currentStudent(r, communityID, id); !ok {
			return errNotStudent(id)
		}
	}
	if err := s.store.SetGroupMembers(r.Context(), groupID, members, personFromContext(r).ID); err != nil {
		return err
	}
	g, err := s.store.GetCoachingGroup(r.Context(), groupID)
	if err != nil || g.EventID == nil {
		return err
	}
	for _, id := range members {
		if err := s.store.RegisterForEvent(r.Context(), *g.EventID, id, 0); err != nil {
			return err
		}
	}
	return nil
}

type errNotStudent int64

func (e errNotStudent) Error() string { return "not a student of this community" }

// handlePutCoachingGroup is PUT /api/communities/{id}/groups/{groupID}.
func (s *Server) handlePutCoachingGroup(w http.ResponseWriter, r *http.Request) {
	_, g, _, ok := s.coachingGroup(w, r, true)
	if !ok {
		return
	}
	var req coachingGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		httpError(w, http.StatusBadRequest, "a group needs a name")
		return
	}
	starts, err1 := parseDay(req.StartsOn)
	ends, err2 := parseDay(req.EndsOn)
	if err1 != nil || err2 != nil {
		httpError(w, http.StatusBadRequest, "invalid dates")
		return
	}
	if err := s.store.UpdateCoachingGroup(r.Context(), g.ID, req.Name, starts, ends); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePutCoachingGroupMembers is PUT /api/communities/{id}/groups/{groupID}/members.
func (s *Server) handlePutCoachingGroupMembers(w http.ResponseWriter, r *http.Request) {
	community, g, _, ok := s.coachingGroup(w, r, true)
	if !ok {
		return
	}
	var req struct {
		Members []int64 `json:"members"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.setGroupMembers(r, community.ID, g.ID, req.Members); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteCoachingGroup is DELETE /api/communities/{id}/groups/{groupID}.
func (s *Server) handleDeleteCoachingGroup(w http.ResponseWriter, r *http.Request) {
	_, g, _, ok := s.coachingGroup(w, r, true)
	if !ok {
		return
	}
	if err := s.store.DeleteCoachingGroup(r.Context(), g.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
