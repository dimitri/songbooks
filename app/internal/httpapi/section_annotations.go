package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

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
			next(w, r.WithContext(context.WithValue(r.Context(), personCtxKey, s.devAutoLogin)))
			return
		}
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			if p, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), personCtxKey, p))
			}
		}
		next(w, r)
	}
}

// handleCreateSectionAnnotation is POST
// /api/variants/{id}/sections/{index}/annotations — requireAuth; any
// logged-in person may annotate any variant they can see (not gated by
// CanEditVariant the way an actual content edit is — annotating never
// touches the variant's own body/grid/lyrics, just a per-section note).
func (s *Server) handleCreateSectionAnnotation(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Body       string `json:"body"`
		Visibility string `json:"visibility"` // "private" (default) or "shared"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		httpError(w, http.StatusBadRequest, "missing body")
		return
	}
	if req.Visibility != "" && req.Visibility != "private" && req.Visibility != "shared" {
		httpError(w, http.StatusBadRequest, `visibility must be "private" or "shared"`)
		return
	}
	id, err := s.store.CreateSectionAnnotation(r.Context(), variantID, sectionIndex, person.ID, req.Body, req.Visibility)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
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
