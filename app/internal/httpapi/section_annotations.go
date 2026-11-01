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
// deny the request if one isn't. Used ahead of handlers (section
// annotations) that accept EITHER a normal login session OR a valid jam
// session: the handler itself resolves whichever identity is present and
// decides access, rather than a single middleware enforcing one specific
// kind of auth up front. This is the HTTP-layer adaptation the brief
// anticipated might be needed: requireAuth/requireRole have no notion of
// "or a jam session" built in, and jam access has never touched the
// variant-edit/annotation routes before this phase, so rather than
// stretch requireAuth itself, this is a separate, narrower middleware
// used only here.
func (s *Server) optionalPerson(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			if p, err := s.store.GetSession(r.Context(), cookie.Value); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), personCtxKey, p))
			}
		}
		next(w, r)
	}
}

// annotationActor resolves "who is making this request" for the
// section-annotation endpoints: a logged-in Person (via optionalPerson,
// above), OR a jam_profiles participant via the jam cookie (see
// jam.go's jamProfileFromCookie — note the cookie's Path is "/", not
// scoped to one /jam/{code} prefix, specifically so it reaches these
// /api/variants/... routes too). Returns personID (0 if none),
// jamEventID/jamProfileID (nil if none) ready to pass straight to
// store.CreateSectionAnnotation, plus ok=false if NEITHER identity is
// present at all (nobody to attribute the note to).
func (s *Server) annotationActor(r *http.Request) (personID *int64, jamEventID *int64, jamProfileID *int64, ok bool) {
	if p := personFromContext(r); p != nil {
		id := p.ID
		return &id, nil, nil, true
	}
	if jp := s.jamProfileFromCookie(r); jp != nil {
		evID, jpID := jp.JamEventID, jp.ID
		return nil, &evID, &jpID, true
	}
	return nil, nil, nil, false
}

// canAnnotateVariant decides who may leave a section_annotations note on
// variantID, for POST /api/variants/{id}/sections/{index}/annotations.
//
// Deliberate permission decision (documented per the brief's explicit
// ask): ANY identified viewer may annotate — logged-in person or jam
// participant alike — NOT gated by CanEditVariant/
// CanEditVariantAsJamParticipant the way actual content edits
// (handleUpdateVariant) are. Annotating is exactly the brief's own
// example of a lightweight, restricted-but-safe action ("edit mode that
// only allows taking notes, per section") — it never touches the
// variant's own body/grid/lyrics, so it doesn't need edit rights on the
// variant, only SOME identity to attribute the note to. This is also
// what makes it usable later as the data model for a dedicated
// "annotation-only editor" role (explicitly out of scope to BUILD this
// phase, per the brief) without needing any schema change then.
//
// The one restriction kept: a jam participant may only annotate a
// variant actually scoped to THEIR event when the variant IS jam-scoped
// (visibility='jam') — a session for jam A shouldn't be usable to leave
// notes "as that jam" on jam B's variant. A variant that ISN'T
// jam-scoped (global/user/band) has no such restriction: any identified
// viewer (logged in, or holding a jam session for ANY event) may
// annotate it, since there's no competing jam-scoping to protect there —
// consistent with how a logged-in person may view (and, per
// CanEditVariant, even edit) any global variant today.
func (s *Server) canAnnotateVariant(ctx context.Context, variant *store.CatalogVariant, jamEventID *int64) (bool, error) {
	if variant.Visibility != "jam" {
		return true, nil
	}
	return jamEventID != nil && variant.OwnerJamEventID != nil && *jamEventID == *variant.OwnerJamEventID, nil
}

// handleCreateSectionAnnotation is POST
// /api/variants/{id}/sections/{index}/annotations — see
// canAnnotateVariant's doc comment for who may call this and why.
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
	personID, jamEventID, jamProfileID, ok := s.annotationActor(r)
	if !ok {
		httpError(w, http.StatusUnauthorized, "login, or a jam session, required")
		return
	}
	variant, err := s.store.GetVariant(r.Context(), variantID)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	can, err := s.canAnnotateVariant(r.Context(), variant, jamEventID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !can {
		httpError(w, http.StatusForbidden, "not a participant of this variant's jam")
		return
	}

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
	id, err := s.store.CreateSectionAnnotation(r.Context(), variantID, sectionIndex, personID, jamEventID, jamProfileID, req.Body, req.Visibility)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handleListSectionAnnotations is GET
// /api/variants/{id}/sections/{index}/annotations — one section's notes.
// Same "any identified viewer" gate as the POST handler, for symmetry
// (reading is no more sensitive than writing here).
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
	personID, _, jamProfileID, ok := s.annotationActor(r)
	if !ok {
		httpError(w, http.StatusUnauthorized, "login, or a jam session, required")
		return
	}
	notes, err := s.store.ListSectionAnnotations(r.Context(), variantID, sectionIndex, personID, jamProfileID)
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
	personID, _, jamProfileID, ok := s.annotationActor(r)
	if !ok {
		httpError(w, http.StatusUnauthorized, "login, or a jam session, required")
		return
	}
	notes, err := s.store.ListAllSectionAnnotations(r.Context(), variantID, personID, jamProfileID)
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
