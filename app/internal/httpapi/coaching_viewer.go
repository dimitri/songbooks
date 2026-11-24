package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"app/internal/i18n"
	"app/internal/store"
)

// Who a coaching community's pages are for (docs/coaching.md, "Who sees
// what"). A student sees the student's pages, their own data; the staff
// (whoever has a manage privilege) the coach's. A site admin who is a
// student there (the app's owner, taking lessons) sees the student's
// pages too; a site admin who isn't a member, the coach's.
//
// A site admin may also look as someone else ("See as…", across every
// page of the studio, remembered in a cookie until they leave it): as the
// coach, as one student by name, or as a member who has nothing yet. A
// student's or a member's preview is read-only: acting would be acting
// for someone who didn't (coachingContext refuses every change while it
// is on). As the coach, the admin acts as themselves, with the coach's
// pages: the actor is the admin, as before. Nobody else's cookie counts.

const seeAsCookie = "coach_see_as"

// coachingViewer is who a coaching community's pages are for.
type coachingViewer struct {
	Coach bool
	// PersonID is whose student pages and data they are: the student's
	// own, the student an admin looks as; 0 for a member with nothing
	// yet, and for the coach.
	PersonID int64
	// Preview is what a site admin looks as: "" (themselves), "coach",
	// "student" or "member"; PreviewName who, for the banner.
	Preview     string
	PreviewName string
	ReadOnly    bool
	// Admin: the site admin, who may look as someone else.
	Admin bool
}

// seeAsValue is what the cookie says for communityID, "" for none.
func seeAsValue(r *http.Request, communityID int64) string {
	c, err := r.Cookie(seeAsCookie)
	if err != nil {
		return ""
	}
	id, as, ok := strings.Cut(c.Value, ":")
	if !ok || id != strconv.FormatInt(communityID, 10) {
		return ""
	}
	return as
}

// coachingViewerOf works out who a coaching community's pages are for.
func (s *Server) coachingViewerOf(r *http.Request, communityID int64) (coachingViewer, error) {
	ctx := r.Context()
	person := personFromContext(r)
	v := coachingViewer{Admin: person.IsAdmin}
	if person.IsAdmin {
		switch as := seeAsValue(r, communityID); {
		case as == "coach":
			return coachingViewer{Coach: true, Preview: "coach", Admin: true}, nil
		case as == "member":
			return coachingViewer{Preview: "member", ReadOnly: true, Admin: true}, nil
		case strings.HasPrefix(as, "student:"):
			id, err := strconv.ParseInt(strings.TrimPrefix(as, "student:"), 10, 64)
			if err == nil {
				if m, ok := s.currentStudent(r, communityID, id); ok {
					return coachingViewer{PersonID: id, Preview: "student", PreviewName: m.DisplayName, ReadOnly: id != person.ID, Admin: true}, nil
				}
			}
		}
	}
	staff, err := s.store.HasAnyCommunityManagePrivilege(ctx, communityID, person.ID)
	if err != nil {
		return v, err
	}
	switch _, student := s.currentStudent(r, communityID, person.ID); {
	case staff:
		v.Coach = true
	case student:
		v.PersonID = person.ID
	case person.IsAdmin:
		v.Coach = true
	default:
		v.PersonID = person.ID
	}
	return v, nil
}

// seeAsOption is one choice of the admin's "See as…".
type seeAsOption struct {
	Value, Label string
	Current      bool
}

// seeAsSwitch is the admin's "See as…" on a coaching community's pages:
// data["Space"].SeeAs, the space-nav template's bar.
type seeAsSwitch struct {
	Action   string // the switch's address
	Back     string // the page to come back to
	Preview  string
	Name     string
	ReadOnly bool
	Options  []seeAsOption
}

// seeAsFor is the switch for a site admin on a coaching community's page,
// nil for anyone else.
func (s *Server) seeAsFor(r *http.Request, community *store.Community, v coachingViewer) *seeAsSwitch {
	if !v.Admin {
		return nil
	}
	lang := s.langOfRequest(r)
	cur := seeAsValue(r, community.ID)
	sw := &seeAsSwitch{Action: fmt.Sprintf("/communities/%d/see-as", community.ID), Back: r.URL.RequestURI(),
		Preview: v.Preview, Name: v.PreviewName, ReadOnly: v.ReadOnly}
	add := func(value, label string) {
		sw.Options = append(sw.Options, seeAsOption{Value: value, Label: label, Current: value == cur})
	}
	add("", i18n.T(lang, "Myself"))
	add("coach", i18n.T(lang, "The coach"))
	if students, err := s.communityStudents(r, community.ID); err == nil {
		for _, m := range students {
			add(fmt.Sprintf("student:%d", m.PersonID), i18n.T(lang, "Student: %s", m.DisplayName))
		}
	}
	add("member", i18n.T(lang, "A member with nothing yet"))
	return sw
}

// handleSeeAs is GET /communities/{id}/see-as?as=…&back=…: a site admin
// looks at a coaching community as someone else, or as themselves again
// (as=""). Anyone else: 403.
func (s *Server) handleSeeAs(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	if !person.IsAdmin {
		httpError(w, http.StatusForbidden, "site admins only")
		return
	}
	communityID, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	community, err := s.store.GetCommunity(r.Context(), communityID)
	if err != nil || !community.IsCoaching() {
		http.NotFound(w, r)
		return
	}
	as := r.URL.Query().Get("as")
	home := fmt.Sprintf("/communities/%d", communityID)
	back := r.URL.Query().Get("back")
	if back != home && !strings.HasPrefix(back, home+"/") && !strings.HasPrefix(back, home+"?") {
		back = home
	}
	switch {
	case as == "":
		http.SetCookie(w, &http.Cookie{Name: seeAsCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	case as == "coach" || as == "member" || strings.HasPrefix(as, "student:"):
		if strings.HasPrefix(as, "student:") {
			id, err := strconv.ParseInt(strings.TrimPrefix(as, "student:"), 10, 64)
			if _, ok := s.currentStudent(r, communityID, id); err != nil || !ok {
				httpError(w, http.StatusBadRequest, "not a student here")
				return
			}
		}
		http.SetCookie(w, &http.Cookie{Name: seeAsCookie, Value: fmt.Sprintf("%d:%s", communityID, as), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
		// A page of one student's, or of the coach's only, may not be
		// there for whom they now look as: the Songbooks section is.
		if as != "coach" && (strings.Contains(back, "/students") || strings.Contains(back, "/student-songs") || strings.Contains(back, "/groups/")) {
			back = home + "/songbooks"
		}
	default:
		httpError(w, http.StatusBadRequest, "see as whom?")
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
