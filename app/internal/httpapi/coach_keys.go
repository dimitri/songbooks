package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"app/internal/chordsym"
	"app/internal/i18n"
	"app/internal/song"
	"app/internal/songimport"
	"app/internal/store"
)

// songKeyName is a song's key: its key: header, or else the key its
// chords are written in; "" when there's no telling.
func songKeyName(sg *song.Song) string {
	if k := sg.Get("key"); k != "" {
		return k
	}
	k, _ := songimport.ChordKey(song.UsedChords(sg))
	return k
}

// songKeyOf is songKeyName for a song by id.
func (s *Server) songKeyOf(ctx context.Context, variantID int64) string {
	body, err := s.store.SongBody(ctx, variantID)
	if err != nil {
		return ""
	}
	return songKeyName(song.Parse(body))
}

// withKeys fills in the songs' keys that their key: header doesn't give.
func (s *Server) withKeys(ctx context.Context, songs []store.StudentSong) []store.StudentSong {
	for i := range songs {
		if songs[i].Key == "" {
			songs[i].Key = s.songKeyOf(ctx, songs[i].VariantID)
		}
	}
	return songs
}

// A coach gives a song its key for a student or a group from the song
// page itself (the player's Key panel), where they try keys by ear: the
// students and groups of the coaching communities the song belongs to,
// each with the key it has for them, and a save. The song then opens in
// that key for the student, the chart and the recording (handleSong).

type coachKeyTarget struct {
	store.KeyTarget
	KeyName string `json:"keyName"` // the song's key moved by Semitones, "" when it has none
	// The student's (or group's) next lesson within sessionWindow, from
	// the community's calendar: they come first, the soonest first, the
	// date said ("jeu. 21 nov."). Nil for none that soon.
	NextSession      *time.Time `json:"nextSession,omitempty"`
	NextSessionLabel string     `json:"nextSessionLabel,omitempty"`
}

// sessionWindow is how far ahead a coach's calendar puts students first
// when they give a song's key: the coming week's lessons.
const sessionWindow = 7 * 24 * time.Hour

// orderByCalendar puts the targets with a lesson coming first, the
// soonest first, and keeps the others in their order (students by name,
// then groups).
func orderByCalendar(ts []coachKeyTarget) {
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i].NextSession, ts[j].NextSession
		if a == nil || b == nil {
			return a != nil && b == nil
		}
		return a.Before(*b)
	})
}

// coachKeyTargets lists what the person coaches for this song. With
// lessonsIn, a coaching community with its key per student on
// (store.PlayerOptions.CoachStudentKeys) whose songbook the song was
// opened from: that community's students and groups only, the coming
// week's lessons first. Else, everyone they coach for it, in the
// communities' order, students by name, then groups.
func (s *Server) coachKeyTargets(r *http.Request, variantID, lessonsIn int64) (string, []coachKeyTarget, error) {
	ctx := r.Context()
	person := personFromContext(r)
	key := s.songKeyOf(ctx, variantID)
	communities, err := s.store.CoachingCommunitiesOfSong(ctx, variantID)
	if err != nil {
		return "", nil, err
	}
	out := []coachKeyTarget{}
	lang := s.langOfRequest(r)
	now := time.Now()
	for _, c := range communities {
		if lessonsIn != 0 && c.ID != lessonsIn {
			continue
		}
		if !person.IsAdmin {
			ok, err := s.store.HasAnyCommunityManagePrivilege(ctx, c.ID, person.ID)
			if err != nil {
				return "", nil, err
			}
			if !ok {
				continue
			}
		}
		ts, err := s.store.KeyTargets(ctx, c, variantID)
		if err != nil {
			return "", nil, err
		}
		var people, groups map[int64]time.Time
		if lessonsIn != 0 {
			if people, groups, err = s.store.UpcomingSessions(ctx, c.ID, now, now.Add(sessionWindow)); err != nil {
				return "", nil, err
			}
		}
		for _, t := range ts {
			ct := coachKeyTarget{KeyTarget: t, KeyName: chordsym.KeyIn(key, t.Semitones)}
			at, ok := people[t.PersonID]
			if t.Kind == "group" {
				at, ok = groups[t.GroupID]
			}
			if ok {
				ct.NextSession, ct.NextSessionLabel = &at, i18n.Date(lang, at, "Mon 2 Jan")
			}
			out = append(out, ct)
		}
	}
	if lessonsIn != 0 {
		orderByCalendar(out)
	}
	return key, out, nil
}

// handleGetCoachKeys is GET /api/songs/{id}/coach-keys[?community=N]:
// whom the person can give this song's key to, and the key each has; an
// empty list for anyone who coaches none of them. ?community= is the
// coaching community whose songbook the song page was opened from: with
// its key per student on, its students only, the coming week's lessons
// first (coachKeyTargets); else as without it.
func (s *Server) handleGetCoachKeys(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var lessonsIn int64
	if id, err := strconv.ParseInt(r.URL.Query().Get("community"), 10, 64); err == nil && id > 0 {
		if o, err := s.store.CommunityPlayerOptions(r.Context(), id); err == nil && o.CoachStudentKeys {
			lessonsIn = id
		}
	}
	key, targets, err := s.coachKeyTargets(r, variantID, lessonsIn)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "targets": targets})
}

// handlePostCoachKey is POST /api/songs/{id}/coach-keys {communityId,
// personId | groupId, semitones}: the song in that key for that student or
// group, added to their songbook when it isn't there yet (a student's
// songbook made the first time, as on their page).
func (s *Server) handlePostCoachKey(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		CommunityID int64 `json:"communityId"`
		PersonID    int64 `json:"personId"`
		GroupID     int64 `json:"groupId"`
		Semitones   int   `json:"semitones"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Semitones < -12 || req.Semitones > 12 {
		httpError(w, http.StatusBadRequest, "key out of range")
		return
	}
	_, targets, err := s.coachKeyTargets(r, variantID, 0)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var t *coachKeyTarget
	for i := range targets {
		x := targets[i]
		if x.CommunityID == req.CommunityID && ((req.PersonID != 0 && x.PersonID == req.PersonID) || (req.GroupID != 0 && x.GroupID == req.GroupID)) {
			t = &targets[i]
			break
		}
	}
	if t == nil {
		httpError(w, http.StatusForbidden, "not one of your students or groups for this song")
		return
	}
	ctx := r.Context()
	me := personFromContext(r)
	sbID := t.SongbookID
	if sbID == 0 {
		if sbID, err = s.store.StudentSongbook(ctx, t.CommunityID, t.PersonID, i18n.T(s.langOfRequest(r), "%s’s songs", t.Name), me.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if !t.InSongbook {
		// The student's songbook is the community's: it links public
		// songs and the community's own (store.LinkCheck).
		if !s.writeLinkErr(w, r, s.store.LinkCheckFor(ctx, me.ID, variantID, store.CommunityOwner(t.CommunityID))) {
			return
		}
		if err := s.store.AddToSongbook(ctx, sbID, variantID, me.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.store.SetEntrySemitones(ctx, sbID, variantID, req.Semitones); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := s.songKeyOf(ctx, variantID)
	writeJSON(w, http.StatusOK, map[string]any{"songbookId": sbID, "semitones": req.Semitones, "keyName": chordsym.KeyIn(key, req.Semitones)})
}
