package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"app/internal/store"
)

var nonSlugRE = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugRE.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// handleSaveOrder persists a book's song order (drag-reorder on the
// project page). Takes the full group list rather than a single move, so
// the client just sends its current DOM order after a drop — simpler than
// reconciling a single move server-side, and the whole file is small.
func (s *Server) handleSaveOrder(w http.ResponseWriter, r *http.Request) {
	book := r.PathValue("book")
	var req struct {
		Groups []struct {
			Label string   `json:"label"`
			Stems []string `json:"stems"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	groups := make([]store.SongGroup, len(req.Groups))
	for i, g := range req.Groups {
		groups[i] = store.SongGroup{Label: g.Label, Stems: g.Stems}
	}
	if err := s.store.SaveOrder(book, groups); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (s *Server) handleCreateBook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "missing name")
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := s.store.CreateBook(name); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p := personFromContext(r); p != nil {
		if err := s.store.AddMember(r.Context(), name, p.ID, "owner"); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (s *Server) handleDeleteBook(w http.ResponseWriter, r *http.Request) {
	book := r.PathValue("book")
	if err := s.store.DeleteBook(book); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleDeleteSong(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	if err := s.store.DeleteSong(book, stem); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleCreateSong(w http.ResponseWriter, r *http.Request) {
	book := r.PathValue("book")
	var req struct {
		Title  string `json:"title"`
		Artist string `json:"artist"`
		Stem   string `json:"stem"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	stem := slugify(req.Stem)
	if stem == "" {
		stem = slugify(req.Title)
	}
	if stem == "" {
		httpError(w, http.StatusBadRequest, "couldn't derive a filename from the title")
		return
	}
	if err := s.store.CreateSong(book, stem, req.Title, req.Artist, actorID(r)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"book": book, "stem": stem})
}

// handleRedoSong is the destructive "redo this song from scratch" action
// (see song.js's confirm-then-call wiring on the edit drawer): it resets
// an existing song's body back to the blank template a brand-new song
// starts from — same book/stem identity, same row, but every bit of
// content discarded — rather than creating a duplicate. Same
// optimistic-concurrency version check as the raw/structured save paths,
// so a stale client can't blow away a save someone else just made.
func (s *Server) handleRedoSong(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	text, newVersion, err := s.store.RedoSong(book, stem, req.Version, actorID(r))
	if errors.Is(err, store.ErrConflict) {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion, "text": text})
}
