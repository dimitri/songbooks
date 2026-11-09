package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"app/internal/song"
	"app/internal/store"
	"app/internal/tab"
)

type songResponse struct {
	Version string   `json:"version"`
	Song    jsonSong `json:"song"`
}

func (s *Server) handleGetSong(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	sg, version, err := s.store.LoadVariant(id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, songResponse{Version: version, Song: toJSON(sg)})
}

type putSongRequest struct {
	Version string   `json:"version"`
	Song    jsonSong `json:"song"`
}

func (s *Server) handlePutSong(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req putSongRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	sg := fromJSON(req.Song)
	newVersion, err := s.store.SaveVariant(id, sg, req.Version, actorID(r))
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion})
}

type rawResponse struct {
	Version string `json:"version"`
	Text    string `json:"text"`
}

func (s *Server) handleGetRaw(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	text, version, err := s.store.LoadVariantRaw(id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rawResponse{Version: version, Text: text})
}

func (s *Server) handlePutRaw(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req rawResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	newVersion, err := s.store.SaveVariantRaw(id, req.Text, req.Version, actorID(r))
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion})
}

// handlePutMeta updates one whitelisted header field (e.g. "reference") in
// place, without the caller needing to round-trip the whole song/raw text —
// used by small single-field UI like the reference-URL input, which has no
// reason to know or preserve a version token it never fetched. Reads the
// current version itself immediately before saving, so this is a
// read-then-write against whatever's on disk right now, not a conflict
// check against a client-held version — fine for a single scalar field
// where the chance of a genuine concurrent edit is negligible.
func (s *Server) handlePutMeta(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if !slices.Contains(song.MetaKeys, req.Key) {
		httpError(w, http.StatusBadRequest, "unknown meta key: "+req.Key)
		return
	}
	sg, version, err := s.store.LoadVariant(id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	sg.Set(req.Key, req.Value)
	newVersion, err := s.store.SaveVariant(id, sg, version, actorID(r))
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion})
}

// handlePutSectionScore is PUT /api/songs/{id}/sections/{index}/score/{label}
// — upserts one ScoreVoice (song.ScoreVoice) into a section by label,
// body {"body": "..."}. Used by the GP import wizard (gp-import.js) to
// write an imported riff straight into the section's own score[label]:
// field in the song's raw text — the SAME storage a hand-written
// score[Drum]: ... line uses — instead of a separate per-section
// database row (see migration 0057_retire_song_scores.sql for why that
// table is gone; an imported riff is a JSON-encoded tab.Riff, which
// tab.ParseVoice recognizes and decodes verbatim via
// tab.DecodeScoreSource instead of running it through the LilyPond-
// subset grammar). Like handlePutMeta just above, this reads the
// current version itself immediately before saving rather than checking
// a client-held one: a multi-section import writes several of these in
// a tight sequence from one wizard session, where threading a version
// through each step for conflict detection against a concurrent edit
// (negligible chance, same import session) would be pure overhead.
func (s *Server) handlePutSectionScore(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	label := r.PathValue("label")
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if _, err := tab.ParseVoice(label, req.Body); err != nil {
		httpError(w, http.StatusBadRequest, "couldn't parse this as a score: "+err.Error())
		return
	}
	sg, version, err := s.store.LoadVariant(id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if index < 0 || index >= len(sg.Sections) {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	sec := &sg.Sections[index]
	replaced := false
	for i := range sec.Scores {
		if strings.EqualFold(sec.Scores[i].Label, label) {
			sec.Scores[i].Body = req.Body
			replaced = true
			break
		}
	}
	if !replaced {
		sec.Scores = append(sec.Scores, song.ScoreVoice{Label: label, Body: req.Body})
	}
	newVersion, err := s.store.SaveVariant(id, sg, version, actorID(r))
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion})
}

func writeSaveErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrConflict) {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	httpError(w, http.StatusInternalServerError, err.Error())
}
