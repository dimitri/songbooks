package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"app/internal/i18n"
	"app/internal/store"
)

// The ownership model, steps 6 and 7 (docs/ownership-model.md §5, §8):
// the original's changes offered to its versions (store.SyncWithOriginal),
// frozen copies when a song goes private or is deleted (store.SongUses),
// and band recordings heard by everyone with a public song.

// handleSyncWithOriginal is POST /api/variants/{id}/original-change:
// {"action": "take", "parts": ["s:chorus#1", ...], "originalVersion": 12}
// takes those parts of the original's change into this version; "keep"
// keeps the version as it is. Either way the version is synced with the
// original at originalVersion (409 when it changed again since).
func (s *Server) handleSyncWithOriginal(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song id")
		return
	}
	var req struct {
		Action          string   `json:"action"`
		Parts           []string `json:"parts"`
		OriginalVersion int      `json:"originalVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	var take []string
	switch req.Action {
	case "take":
		if len(req.Parts) == 0 {
			httpError(w, http.StatusBadRequest, "no part to take")
			return
		}
		take = req.Parts
	case "keep":
	default:
		httpError(w, http.StatusBadRequest, `action is "take" or "keep"`)
		return
	}
	changed, err := s.store.SyncWithOriginal(r.Context(), personFromContext(r).ID, id, req.OriginalVersion, take)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "the original changed again", "message": i18n.T(s.langOfRequest(r), "The original changed again meanwhile: look at it again."),
		})
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"changed": changed})
	}
}

// handleSongUses is GET /api/variants/{id}/uses?for=private|delete: who
// would lose the song, said as the confirmation says it ("Make “Gin Lane”
// private? 3 songbooks link to it and 1 band has its own version. …").
func (s *Server) handleSongUses(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song id")
		return
	}
	forDelete := r.URL.Query().Get("for") == "delete"
	u, err := s.store.SongUsesFor(r.Context(), id, forDelete)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v, err := s.store.GetVariant(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message":   songUsesMessage(s.langOfRequest(r), v.Title, u, forDelete),
		"songbooks": u.Songbooks, "setlists": u.Setlists, "owners": len(u.Owners),
		"versions": u.BandVersions + u.PersonVersions + u.CommunityVersions,
	})
}

// songUsesMessage is the confirmation before making title private, or
// deleting it.
func songUsesMessage(lang, title string, u *store.SongUses, forDelete bool) string {
	q := i18n.T(lang, "Make “%s” private?", title)
	if forDelete {
		q = i18n.T(lang, "Delete “%s”? This can’t be undone.", title)
	}
	if !u.Any() {
		if forDelete {
			return q
		}
		return q + " " + i18n.T(lang, "Only its owner’s members will see it.")
	}
	msg := q + " " + i18n.T(lang, "%s.", capitalize(u.Sentence(lang)))
	if len(u.Owners) > 0 {
		msg += " " + i18n.T(lang, "Their songbooks keep a frozen copy of it as it is now, and their set lists keep working; nobody else will find it any more.")
	}
	if u.BandVersions+u.PersonVersions+u.CommunityVersions > 0 {
		msg += " " + i18n.T(lang, "Their own arrangements stay theirs; they won’t be offered its changes any more.")
	}
	return msg
}

func capitalize(s string) string {
	for i, r := range s {
		if r >= 'a' && r <= 'z' {
			return s[:i] + string(r-'a'+'A') + s[i+1:]
		}
		return s
	}
	return s
}

// handleSongRecordingsPage is GET /song/{id}/recordings: the bands'
// recordings of a song everyone signed in hears (§8), to listen to with
// the band recordings' player.
func (s *Server) handleSongRecordingsPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	v, err := s.store.GetVariant(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	bands, err := s.store.PublicSongRecordings(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "song_public_recordings", map[string]any{
		"Me": personFromContext(r), "Song": v, "Bands": bands,
	})
}

// handleSetBandRecordingPublic is POST
// /api/bands/{id}/recordings/{recID}/public {"public": true}: a member
// whose role grants publish (by default, a core member) makes one
// recording of a public song the band links public, or not (§8).
func (s *Server) handleSetBandRecordingPublic(w http.ResponseWriter, r *http.Request) {
	bandID, _ := pathID(r, "id")
	recID, err := pathID(r, "recID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid recording id")
		return
	}
	// The route holds it to a member whose role grants publish.
	var req struct {
		Public bool `json:"public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	switch err := s.store.SetRecordingPublic(r.Context(), bandID, recID, req.Public); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrInvalid):
		httpError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"public": req.Public})
	}
}

// songOwnershipData adds to the song page what steps 6 and 7 show:
// OriginalChange (the original changed since this version synced, for who
// edits it), Frozen (a frozen copy: of what, when), PlayedBy (the bands
// whose recordings of a public song everyone hears).
func (s *Server) songOwnershipData(r *http.Request, data map[string]any, variantID int64, variant *store.CatalogVariant) {
	ctx := r.Context()
	person := personFromContext(r)
	if person != nil {
		if c, err := s.store.OriginalChangeFor(ctx, person.ID, variantID); err == nil && c != nil {
			data["OriginalChange"] = c
		}
	}
	if f, err := s.store.FrozenOf(ctx, variantID); err == nil && f != nil {
		data["Frozen"] = f
	}
	if variant != nil && variant.Public {
		if by, err := s.store.PlayedByFor(ctx, []int64{variantID}); err == nil && len(by[variantID]) > 0 {
			data["PlayedBy"] = by[variantID]
		}
	}
}
