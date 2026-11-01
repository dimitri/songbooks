package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"app/internal/song"
	"app/internal/store"
)

type songResponse struct {
	Version string   `json:"version"`
	Song    jsonSong `json:"song"`
}

func (s *Server) handleGetSong(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	sg, version, err := s.store.LoadSong(book, stem)
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
	book, stem := r.PathValue("book"), r.PathValue("stem")
	var req putSongRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	sg := fromJSON(req.Song)
	newVersion, err := s.store.SaveSong(book, stem, sg, req.Version, actorID(r))
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
	book, stem := r.PathValue("book"), r.PathValue("stem")
	text, version, err := s.store.LoadRaw(book, stem)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rawResponse{Version: version, Text: text})
}

func (s *Server) handlePutRaw(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	var req rawResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	newVersion, err := s.store.SaveRaw(book, stem, req.Text, req.Version, actorID(r))
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
	book, stem := r.PathValue("book"), r.PathValue("stem")
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
	sg, version, err := s.store.LoadSong(book, stem)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	sg.Set(req.Key, req.Value)
	newVersion, err := s.store.SaveSong(book, stem, sg, version, actorID(r))
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
