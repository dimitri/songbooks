package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Live lessons (docs/live-lessons.md, docs/coaching.md): an online
// lesson meets on Galène, in its own room. The app's join link checks
// who is joining and that it's time, signs a short token Galène trusts,
// and sends the browser into the room: no password, nothing to set up.
// The link is ours, not Galène's, so the one in a calendar app still
// works weeks later and follows a rescheduled lesson.

// GaleneConfig is where Galène answers and the key its lessons group
// checks our tokens with (deploy/.env: SONGBOOKS_GALENE_URL,
// SONGBOOKS_GALENE_KEY; galene/setup.sh writes the key on both sides).
type GaleneConfig struct {
	URL string // https://live.songbooks.studio, no trailing slash
	Key []byte
	KID string // the key's id in groups/lessons.json
}

// Galene is this server's, set by main; no key, no live lessons.
var Galene GaleneConfig

// ParseGaleneKey reads a key as galene/setup.sh writes it: a JWK "k",
// base64url without padding.
func ParseGaleneKey(k string) ([]byte, error) {
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(k), "="))
	if err != nil {
		return nil, err
	}
	if len(key) < 16 {
		return nil, errors.New("the Galène key is too short")
	}
	return key, nil
}

func (g GaleneConfig) ready() bool { return g.URL != "" && len(g.Key) > 0 }

// roomURL is a lesson's room: exactly the URL Galène expects as a
// token's audience, trailing slash included.
func (g GaleneConfig) roomURL(room string) string {
	return g.URL + "/group/lessons/" + room + "/"
}

// What a coach and a student may do in the room: the coach runs it
// (mutes, removes, lets someone in with a token of their own), a
// student shows themselves and talks.
var (
	coachPermissions   = []string{"op", "present", "message", "caption", "token"}
	studentPermissions = []string{"present", "message"}
)

// liveTokenTTL is how long a token can be used to enter the room: the
// redirect follows at once, and a token isn't needed once in.
const liveTokenTTL = 2 * time.Minute

// token signs a Galène token (a JWT, HS256): sub the name shown in the
// room, aud the room.
func (g GaleneConfig) token(aud, sub string, permissions []string, now time.Time) (string, error) {
	enc := base64.RawURLEncoding
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT", "kid": g.KID})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"sub":         sub,
		"aud":         aud,
		"permissions": permissions,
		"iat":         now.Unix(),
		"exp":         now.Add(liveTokenTTL).Unix(),
	})
	if err != nil {
		return "", err
	}
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)
	mac := hmac.New(sha256.New, g.Key)
	mac.Write([]byte(signing))
	return signing + "." + enc.EncodeToString(mac.Sum(nil)), nil
}

// lessonJoinPath is a lesson's join link, on this site.
func lessonJoinPath(communityID, eventID int64) string {
	return "/communities/" + strconv.FormatInt(communityID, 10) + "/lessons/" + strconv.FormatInt(eventID, 10) + "/join"
}

// handleLessonJoin is GET /communities/{id}/lessons/{eventID}/join: the
// lesson's coach or one of its students, logged in or known by their
// calendar feed's address (?f=, the link in their calendar app), is
// sent into the lesson's room from 15 minutes before it starts to 30
// minutes after it ends; otherwise a page says when. ?headphones=1 or 0
// changes, and remembers, how they join.
func (s *Server) handleLessonJoin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	person := personFromContext(r)
	if person == nil {
		if f := r.URL.Query().Get("f"); f != "" {
			id, err := s.store.PersonByCalendarToken(ctx, f)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if id != 0 {
				if person, err = s.store.GetPersonByID(ctx, id); err != nil {
					httpError(w, http.StatusInternalServerError, err.Error())
					return
				}
				setLangPerson(w, person)
			}
		}
	}
	if person == nil {
		s.denyUnauthenticated(w, r)
		return
	}
	communityID, err1 := pathID(r, "id")
	eventID, err2 := pathID(r, "eventID")
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	community, err := s.store.GetCommunity(ctx, communityID)
	if err != nil || !community.IsCoaching() {
		http.NotFound(w, r)
		return
	}
	ev, err := s.store.GetEvent(ctx, eventID)
	if err != nil || ev.OwnerKind != "community" || ev.OwnerCommunityID != community.ID || ev.EventType != "lesson" {
		http.NotFound(w, r)
		return
	}
	coach := person.IsAdmin || (ev.CreatedByPersonID != nil && *ev.CreatedByPersonID == person.ID)
	if !coach {
		if coach, err = s.store.HasAnyCommunityManagePrivilege(ctx, community.ID, person.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if !coach {
		regs, err := s.store.ListEventRegistrations(ctx, ev.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		student := false
		for _, reg := range regs {
			student = student || reg.PersonID == person.ID
		}
		if !student {
			http.NotFound(w, r)
			return
		}
	}

	headphones, err := s.store.LiveHeadphones(ctx, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h := r.URL.Query().Get("headphones"); h == "1" || h == "0" {
		headphones = h == "1"
		if err := s.store.SetLiveHeadphones(ctx, person.ID, headphones); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	page := map[string]any{
		"Person":    person,
		"Community": community,
		"Event":     ev,
		"Back":      "/communities/" + strconv.FormatInt(community.ID, 10) + "/lessons",
	}
	now := time.Now()
	opens, closes := ev.LiveWindow()
	switch {
	case ev.LessonMode != "online":
		page["Why"] = "in_person"
	case !Galene.ready():
		page["Why"] = "off"
	case now.Before(opens):
		page["Why"], page["Opens"] = "early", opens
	case now.After(closes):
		page["Why"] = "over"
	}
	if page["Why"] != nil {
		s.render(w, "live_join", page)
		return
	}

	room, err := s.store.LiveRoom(ctx, ev.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	perms := studentPermissions
	if coach {
		perms = coachPermissions
	}
	aud := Galene.roomURL(room)
	tok, err := Galene.token(aud, person.DisplayName, perms, now)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := url.Values{"token": {tok}, "headphones": {"0"}}
	if headphones {
		q.Set("headphones", "1")
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, aud+"?"+q.Encode(), http.StatusSeeOther)
}

// handlePutLiveHeadphones is PUT /api/me/live-headphones {"on": bool}:
// how the person joins live lessons, from the Lessons page's switch.
func (s *Server) handlePutLiveHeadphones(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetLiveHeadphones(r.Context(), personFromContext(r).ID, req.On); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
