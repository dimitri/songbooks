package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"app/internal/chordsym"
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
}

// coachKeyTargets lists what the person coaches for this song.
func (s *Server) coachKeyTargets(r *http.Request, variantID int64) (string, []coachKeyTarget, error) {
	ctx := r.Context()
	person := personFromContext(r)
	key := s.songKeyOf(ctx, variantID)
	communities, err := s.store.CoachingCommunitiesOfSong(ctx, variantID)
	if err != nil {
		return "", nil, err
	}
	out := []coachKeyTarget{}
	for _, c := range communities {
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
		for _, t := range ts {
			out = append(out, coachKeyTarget{KeyTarget: t, KeyName: chordsym.KeyIn(key, t.Semitones)})
		}
	}
	return key, out, nil
}

// handleGetCoachKeys is GET /api/songs/{id}/coach-keys: whom the person
// can give this song's key to, and the key each has; an empty list for
// anyone who coaches none of them.
func (s *Server) handleGetCoachKeys(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	key, targets, err := s.coachKeyTargets(r, variantID)
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
	_, targets, err := s.coachKeyTargets(r, variantID)
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
		if sbID, err = s.store.StudentSongbook(ctx, t.CommunityID, t.PersonID, t.Name+"’s songs", me.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if !t.InSongbook {
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
