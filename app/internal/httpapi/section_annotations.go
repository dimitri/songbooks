package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"app/internal/store"
)

// optionalPerson loads the session person into request context if a
// valid session cookie is present, but — unlike requireAuth — does NOT
// deny the request if one isn't. Used ahead of handlers that are
// genuinely public but render differently for a logged-in visitor (a
// community's public events page, event registration) — the handler
// itself checks personFromContext and branches.
func (s *Server) optionalPerson(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Dev-only escape hatch — see Server.devAutoLogin and requireAuth's
		// matching branch. nil (and this branch dead) unless explicitly set.
		if s.devAutoLogin != nil {
			setLangPerson(w, s.devAutoLogin)
			next(w, r.WithContext(context.WithValue(r.Context(), personCtxKey, s.devAutoLogin)))
			return
		}
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			if p, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
				setLangPerson(w, p)
				r = r.WithContext(context.WithValue(r.Context(), personCtxKey, p))
			}
		}
		next(w, r)
	}
}

// maxNoteRunes caps one note's text: a note is a line or a paragraph
// next to a section, not a document.
const maxNoteRunes = 4000

// noteRequest is the body of a create or an edit: the text, who can see
// it ("private", the default, or "shared"), and — on an edit only — the
// section it moves to (nil keeps it where it is).
type noteRequest struct {
	Body         string `json:"body"`
	Visibility   string `json:"visibility"`
	SectionIndex *int   `json:"section_index"`
}

// decodeNoteRequest reads and checks a noteRequest, writing the 400
// itself and returning false when it's unusable.
func decodeNoteRequest(w http.ResponseWriter, r *http.Request) (noteRequest, bool) {
	var req noteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON")
		return req, false
	}
	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" {
		httpError(w, http.StatusBadRequest, "missing body")
		return req, false
	}
	if utf8.RuneCountInString(req.Body) > maxNoteRunes {
		httpError(w, http.StatusBadRequest, "note too long")
		return req, false
	}
	if req.Visibility == "" {
		req.Visibility = "private"
	}
	if req.Visibility != "private" && req.Visibility != "shared" {
		httpError(w, http.StatusBadRequest, `visibility must be "private" or "shared"`)
		return req, false
	}
	if req.SectionIndex != nil && *req.SectionIndex < 0 {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return req, false
	}
	return req, true
}

// handleCreateSectionAnnotation is POST
// /api/variants/{id}/sections/{index}/annotations — any person who can
// view the variant may annotate it (requireVariantRole "viewer"; not
// gated by CanEditVariant the way an actual content edit is — annotating
// never touches the variant's own body/grid/lyrics, just a per-section
// note).
func (s *Server) handleCreateSectionAnnotation(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	sectionIndex, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || sectionIndex < 0 {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	req, ok := decodeNoteRequest(w, r)
	if !ok {
		return
	}
	id, err := s.store.CreateSectionAnnotation(r.Context(), variantID, sectionIndex, personFromContext(r).ID, req.Body, req.Visibility)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handleUpdateSectionAnnotation is PATCH
// /api/variants/{id}/annotations/{note} — the author edits their own
// note: its text, who can see it, and optionally its section. Someone
// else's note (shared ones included) is a 404, the same as a missing one.
func (s *Server) handleUpdateSectionAnnotation(w http.ResponseWriter, r *http.Request) {
	variantID, noteID, ok := variantAndNoteIDs(w, r)
	if !ok {
		return
	}
	req, ok := decodeNoteRequest(w, r)
	if !ok {
		return
	}
	found, err := s.store.UpdateSectionAnnotation(r.Context(), variantID, noteID, personFromContext(r).ID, req.SectionIndex, req.Body, req.Visibility)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		httpError(w, http.StatusNotFound, "no such note")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": noteID})
}

// handleDeleteSectionAnnotation is DELETE
// /api/variants/{id}/annotations/{note} — the author removes their own
// note; anyone else's is a 404.
func (s *Server) handleDeleteSectionAnnotation(w http.ResponseWriter, r *http.Request) {
	variantID, noteID, ok := variantAndNoteIDs(w, r)
	if !ok {
		return
	}
	found, err := s.store.DeleteSectionAnnotation(r.Context(), variantID, noteID, personFromContext(r).ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		httpError(w, http.StatusNotFound, "no such note")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// variantAndNoteIDs parses the {id} and {note} path values, writing the
// 400 itself when either is malformed.
func variantAndNoteIDs(w http.ResponseWriter, r *http.Request) (variantID, noteID int64, ok bool) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return 0, 0, false
	}
	noteID, err = strconv.ParseInt(r.PathValue("note"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid note id")
		return 0, 0, false
	}
	return variantID, noteID, true
}

// handleListSectionAnnotations is GET
// /api/variants/{id}/sections/{index}/annotations — one section's notes.
func (s *Server) handleListSectionAnnotations(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	sectionIndex, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	person := personFromContext(r)
	notes, err := s.store.ListSectionAnnotations(r.Context(), variantID, sectionIndex, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, notes)
}

// handleListAllSectionAnnotations is GET /api/variants/{id}/annotations —
// every section's notes in one call, grouped by section_index in the
// response (ListAllSectionAnnotations already orders by section_index),
// for a page that wants to render every section's notes inline without
// one request per section.
func (s *Server) handleListAllSectionAnnotations(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	person := personFromContext(r)
	notes, err := s.store.ListAllSectionAnnotations(r.Context(), variantID, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	bySection := map[int][]store.SectionAnnotation{}
	for _, n := range notes {
		bySection[n.SectionIndex] = append(bySection[n.SectionIndex], n)
	}
	writeJSON(w, http.StatusOK, bySection)
}
