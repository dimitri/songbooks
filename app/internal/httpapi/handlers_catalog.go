package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"app/internal/store"
)

// handleCatalogPage is the global catalog browse view: every global
// variant plus the viewer's own, filtered by artist/project and sorted
// alphabetically or by artist. "By album" is intentionally not offered —
// no song in this app's data model carries an album field (see
// song.MetaKeys in internal/song/song.go), so there's nothing to group or
// filter by; "by project" is the closest grouping concept that actually
// exists in the data.
func (s *Server) handleCatalogPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	filter := store.CatalogFilter{
		Query:  r.URL.Query().Get("q"),
		Artist: r.URL.Query().Get("artist"),
		Genre:  r.URL.Query().Get("genre"),
		Sort:   r.URL.Query().Get("sort"),
	}
	variants, err := s.store.ListCatalog(r.Context(), person.ID, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	genres, err := s.store.ListGenres(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defaultSongbookID, err := s.store.GetDefaultSongbook(r.Context(), "person", person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.render(w, "catalog", map[string]any{
		"Me": person, "Variants": variants, "Filter": filter, "Genres": genres,
		"DefaultSongbookID": defaultSongbookID,
	})
}

// catalogSearchHit is one result of handleCatalogSearch — just enough to
// render a type-ahead row and carry the variant id the caller actually
// needs (e.g. the band repertoire "Add song" dialog's add/fork actions).
type catalogSearchHit struct {
	VariantID  int64  `json:"variantId"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Visibility string `json:"visibility"`
}

// handleCatalogSearch is GET /api/catalog/search?q=... — a JSON type-
// ahead over the same ListCatalog full-text search handleCatalogPage
// uses, for any UI that needs "find a variant by name" without a full
// page navigation (the band repertoire "Add song" dialog's by-id form
// used to make someone go find a catalog page, copy a numeric id, and
// paste it back — this replaces that with search-as-you-type). Capped
// at maxCatalogSearchResults since it's meant for a dropdown, not a
// browse page.
const maxCatalogSearchResults = 20

func (s *Server) handleCatalogSearch(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []catalogSearchHit{})
		return
	}
	variants, err := s.store.ListCatalog(r.Context(), person.ID, store.CatalogFilter{Query: q})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(variants) > maxCatalogSearchResults {
		variants = variants[:maxCatalogSearchResults]
	}
	hits := make([]catalogSearchHit, len(variants))
	for i, v := range variants {
		hits[i] = catalogSearchHit{VariantID: v.VariantID, Title: v.Title, Artist: v.Artist, Visibility: v.Visibility}
	}
	writeJSON(w, http.StatusOK, hits)
}

// handleSongbooksIndexPage is the "Songbooks" nav entry's destination: every
// songbook the viewer can open, their own personal ones plus each of their
// bands' shared repertoires — split out from the flat all-songs catalog
// (handleCatalogPage) so each has its own URL/nav entry instead of one page
// with an internal tab toggle.
func (s *Server) handleSongbooksIndexPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	mySongbooks, err := s.store.ListSongbooks(r.Context(), "person", person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bands, err := s.store.ListBandsForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bandCards, _, err := s.buildBandCards(r.Context(), person.ID, bands)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Community (school) repertoires were missing from this index
	// entirely — a community member had no way to reach their
	// community's own shared songbook from the main Songbooks page,
	// only the bands+personal sections existed. Member count mirrors
	// bandCardView's own "N members" meta line.
	schools, err := s.store.ListSchoolsForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type schoolCardView struct {
		ID      int64
		Name    string
		Members int
	}
	var schoolCards []schoolCardView
	for _, sc := range schools {
		members, err := s.store.ListSchoolMembers(r.Context(), sc.ID, false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		schoolCards = append(schoolCards, schoolCardView{ID: sc.ID, Name: sc.Name, Members: len(members)})
	}
	s.render(w, "songbooks-index", map[string]any{
		"Me": person, "MySongbooks": mySongbooks, "BandCards": bandCards, "SchoolCards": schoolCards,
	})
}

// handleWorkVersionsPage is a work's "all versions of this song" page:
// every variant the viewer may see (every global one, plus their own
// private ones), with a free-text annotation box/list underneath.
func (s *Server) handleWorkVersionsPage(w http.ResponseWriter, r *http.Request) {
	workID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid work id", http.StatusBadRequest)
		return
	}
	person := personFromContext(r)
	variants, err := s.store.ListVariantsForWork(r.Context(), workID, person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Annotations are per-variant, not per-work — fetch each variant's
	// notes so the page can show them grouped under their variant.
	type variantWithNotes struct {
		store.CatalogVariant
		Annotations []store.Annotation
	}
	var rows []variantWithNotes
	for _, v := range variants {
		notes, err := s.store.ListAnnotations(r.Context(), v.VariantID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rows = append(rows, variantWithNotes{CatalogVariant: v, Annotations: notes})
	}
	s.render(w, "work_versions", map[string]any{
		"Me": person, "WorkID": workID, "Variants": rows,
	})
}

// handleVariantHistoryPage is one variant's revision history, with a
// revert button on every past revision.
func (s *Server) handleVariantHistoryPage(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid variant id", http.StatusBadRequest)
		return
	}
	person := personFromContext(r)
	variant, err := s.store.GetVariant(r.Context(), variantID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	revisions, err := s.store.ListRevisions(r.Context(), "variant", strconv.FormatInt(variantID, 10))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	canEdit, err := s.store.CanEditVariant(r.Context(), person.ID, variantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "variant_history", map[string]any{
		"Me": person, "Variant": variant, "Revisions": revisions, "CanEdit": canEdit,
	})
}

// handleForkVariant creates a personal copy of a variant (global or
// someone else's) in the caller's own catalog, with lineage back to the
// original — a normal HTTP action, not a special "promote" flow.
func (s *Server) handleForkVariant(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	person := personFromContext(r)
	newID, err := s.store.ForkVariant(r.Context(), variantID, person.ID, "user", 0)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"variantId": newID})
}

// handleCreateAnnotation leaves a free-text note on a variant — not an
// edit to its body/grid/lyrics, a comment alongside it.
func (s *Server) handleCreateAnnotation(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		httpError(w, http.StatusBadRequest, "missing body")
		return
	}
	person := personFromContext(r)
	id, err := s.store.AnnotateVariant(r.Context(), variantID, person.ID, req.Body)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handleListRevisions is GET /api/revisions?entity_type=X&entity_id=Y —
// generic revision history for any entity type this phase (or a later
// one) records revisions for.
func (s *Server) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	entityType := r.URL.Query().Get("entity_type")
	entityID := r.URL.Query().Get("entity_id")
	if entityType == "" || entityID == "" {
		httpError(w, http.StatusBadRequest, "entity_type and entity_id are required")
		return
	}
	revisions, err := s.store.ListRevisions(r.Context(), entityType, entityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, revisions)
}

// handleRevertRevision is POST /api/revisions/{id}/revert. Per the
// phase-1 brief, anyone with edit rights on the entity may revert ANY
// revision of it, not just their own — see store.CanEditVariant's
// doc comment for the (deliberately permissive, for now) rule that
// applies to variants.
func (s *Server) handleRevertRevision(w http.ResponseWriter, r *http.Request) {
	revisionID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid revision id")
		return
	}
	person := personFromContext(r)

	entityType, entityID, err := s.store.RevisionEntity(r.Context(), revisionID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	switch entityType {
	case "variant":
		variantID, err := strconv.ParseInt(entityID, 10, 64)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "revision has a non-numeric variant entity_id")
			return
		}
		canEdit, err := s.store.CanEditVariant(r.Context(), person.ID, variantID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !canEdit {
			httpError(w, http.StatusForbidden, "no edit rights on this variant")
			return
		}
	case "band_member":
		// Carries a band_id — peek it off the revision being reverted
		// itself, not off the entity's current row (which, for an "add"
		// revision, this very revert may be about to delete).
		bandID, err := s.store.PeekRevisionBandID(r.Context(), revisionID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		isMember, err := s.store.IsCurrentBandMember(r.Context(), bandID, person.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isMember {
			httpError(w, http.StatusForbidden, "must be a current member of this band")
			return
		}
	case "setlist":
		setlistID, err := strconv.ParseInt(entityID, 10, 64)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "revision has a non-numeric setlist entity_id")
			return
		}
		canEdit, err := s.store.CanEditSetlistByID(r.Context(), person.ID, setlistID)
		if errors.Is(err, store.ErrNotFound) {
			// The setlist row itself may be gone (this revert is undoing
			// its own creation, or a prior revert already deleted it) —
			// fall back to the band_id carried in the revision's own
			// snapshot, same reasoning as the band_member/
			// band_catalog_entry case above. A person-scoped setlist
			// that's gone this way simply can't be reverted any more
			// (no owner to peek at that doesn't require the row), which
			// is fine: there's nothing left to revert to.
			bandID, peekErr := s.store.PeekRevisionBandID(r.Context(), revisionID)
			if peekErr != nil || bandID == 0 {
				httpError(w, http.StatusNotFound, err.Error())
				return
			}
			isMember, memErr := s.store.IsCurrentBandMember(r.Context(), bandID, person.ID)
			if memErr != nil {
				httpError(w, http.StatusInternalServerError, memErr.Error())
				return
			}
			if !isMember {
				httpError(w, http.StatusForbidden, "must be a current member of this band")
				return
			}
		} else if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		} else if !canEdit {
			httpError(w, http.StatusForbidden, "no edit rights on this setlist")
			return
		}
	default:
		httpError(w, http.StatusBadRequest, "revert not supported for entity_type "+entityType)
		return
	}

	if err := s.store.RevertRevision(r.Context(), revisionID, person.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reverted": true})
}

// handleUpdateVariant is PUT /api/variants/{id} — edits a variant's
// content directly by its row id, the path personal-, band-, and
// jam-owned variants are edited through (see store.SaveVariantByID's doc
// comment for why this is a separate path from PUT /api/songs/{book}/
// {stem}: that one is gated on project-editor role, which a fork's
// owner, a band member, or a jam participant need not hold on the
// project the variant happens to live under).
//
// Routed through optionalPerson, not requireAuth: a jam participant
// editing their own jam-scoped creation has no login session at all
// (see jam_profiles) — this handler accepts EITHER a normal login
// session (checked via store.CanEditVariant) OR a valid jam session for
// the variant's own owner_jam_event_id (via
// store.CanEditVariantAsJamParticipant), same "jam's participant roster
// IS its membership" rule CanEditVariant's band branch already uses for
// band_members.
func (s *Server) handleUpdateVariant(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	person := personFromContext(r)
	var canEdit bool
	var actorPersonID int64
	if person != nil {
		canEdit, err = s.store.CanEditVariant(r.Context(), person.ID, variantID)
		actorPersonID = person.ID
	} else if jp := s.jamProfileFromCookie(r); jp != nil {
		canEdit, err = s.store.CanEditVariantAsJamParticipant(r.Context(), jp.JamEventID, variantID)
	} else {
		httpError(w, http.StatusUnauthorized, "login, or a jam session, required")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !canEdit {
		httpError(w, http.StatusForbidden, "no edit rights on this variant")
		return
	}

	var req struct {
		Title         string `json:"title"`
		Artist        string `json:"artist"`
		ReferenceURL  string `json:"referenceUrl"`
		Body          string `json:"body"`
		ExpectVersion string `json:"expectVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	newVersion, err := s.store.SaveVariantByID(r.Context(), variantID, req.Title, req.Artist, req.ReferenceURL, req.Body, req.ExpectVersion, actorPersonID)
	if errors.Is(err, store.ErrConflict) {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion})
}

// handlePutVariantGenre sets the free-text genre on variantID's parent
// WORK (see migration 0030 — genre is a property of the song, not of one
// person's/band's particular arrangement of it), via Prep's compact
// header-fields row alongside time signature/tempo. A standalone
// endpoint rather than folded into handleUpdateVariant's body/title/
// artist/reference save, since genre lives on a different table and
// isn't part of the raw .song-format grammar.
func (s *Server) handlePutVariantGenre(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	person := personFromContext(r)
	canEdit, err := s.store.CanEditVariant(r.Context(), person.ID, variantID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !canEdit {
		httpError(w, http.StatusForbidden, "no edit rights on this variant")
		return
	}
	v, err := s.store.GetVariant(r.Context(), variantID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var req struct {
		Genre string `json:"genre"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetWorkGenre(r.Context(), v.WorkID, strings.TrimSpace(req.Genre)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"genre": req.Genre})
}
