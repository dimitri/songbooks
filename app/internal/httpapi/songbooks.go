package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"app/internal/store"
)

// handleMySongbooksPage lists the logged-in person's own songbooks (the
// "project list"/My songbook page's replacement) — every person has at
// least a default one (see migration 0039).
func (s *Server) handleMySongbooksPage(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	books, err := s.store.ListSongbooks(r.Context(), "person", p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "my-songbooks", map[string]any{"Me": p, "Songbooks": books})
}

// handleCreateSongbook makes a new songbook for the logged-in person, or
// (ownerBandID given) for a band they're a current member of — "a band
// could choose to have a songbook per album, or a single songbook with
// all the songs in it."
func (s *Server) handleCreateSongbook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		OwnerBandID int64  `json:"ownerBandId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "missing name")
		return
	}
	p := personFromContext(r)
	ownerKind, ownerID := "person", p.ID
	if req.OwnerBandID != 0 {
		isMember, err := s.store.IsCurrentBandMember(r.Context(), req.OwnerBandID, p.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isMember {
			httpError(w, http.StatusForbidden, "must be a current member of this band")
			return
		}
		ownerKind, ownerID = "band", req.OwnerBandID
	}
	id, err := s.store.CreateSongbook(r.Context(), ownerKind, ownerID, strings.TrimSpace(req.Name), false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

// handleSongbookPage shows one songbook: its song list (ordered per its
// own song_order, same Set 1/Set 2/... grouping a project used to have),
// playlist link, and (for a band songbook) the usual band-page chrome.
func (s *Server) handleSongbookPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	ok, err := s.store.CanAccessSongbook(r.Context(), p.ID, id)
	if err != nil || !ok {
		http.NotFound(w, r)
		return
	}
	sb, err := s.store.GetSongbook(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	groups, err := s.store.ListSongsOrdered(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type songbookSongRow struct {
		VariantID            int64
		Title, Tempo, Key string
	}
	type songbookGroup struct {
		Label string
		Songs []songbookSongRow
	}
	var out []songbookGroup
	for _, g := range groups {
		sg := songbookGroup{Label: g.Label}
		for _, vid := range g.VariantIDs {
			row := songbookSongRow{VariantID: vid, Title: "untitled"}
			if parsed, _, err := s.store.LoadVariant(vid); err == nil {
				row.Title = orDefault(parsed.Get("title"), "untitled")
				row.Tempo = parsed.Get("tempo")
				row.Key = parsed.Get("key")
			}
			sg.Songs = append(sg.Songs, row)
		}
		out = append(out, sg)
	}
	s.render(w, "songbook", map[string]any{
		"Me": p, "Songbook": sb, "Groups": out,
	})
}

// handleRenameSongbook renames any songbook (default or not) the caller
// can access.
func (s *Server) handleRenameSongbook(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanAccessSongbook(r.Context(), p.ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no access to this songbook")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "missing name")
		return
	}
	if err := s.store.RenameSongbook(r.Context(), id, strings.TrimSpace(req.Name)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleDeleteSongbook deletes a non-default songbook the caller can
// access — "every profile should have a 'my song book' default... can't
// have zero" means the default one is never deletable (store.DeleteSongbook
// enforces this).
func (s *Server) handleDeleteSongbook(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanAccessSongbook(r.Context(), p.ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no access to this songbook")
		return
	}
	if err := s.store.DeleteSongbook(r.Context(), id); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleSaveSongbookOrder persists a songbook's song order (drag-reorder
// on the songbook page).
func (s *Server) handleSaveSongbookOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanAccessSongbook(r.Context(), p.ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no access to this songbook")
		return
	}
	var req struct {
		Groups []struct {
			Label      string  `json:"label"`
			VariantIDs []int64 `json:"variantIds"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	groups := make([]store.SongGroup, len(req.Groups))
	for i, g := range req.Groups {
		groups[i] = store.SongGroup{Label: g.Label, VariantIDs: g.VariantIDs}
	}
	if err := s.store.SaveOrder(r.Context(), id, groups); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleCreateSongInSongbook creates a brand-new variant owned by
// songbookID's owner (private, or band-private) and adds it to that
// songbook — "it is possible to create a new song, and at creation we
// need to pick private, band private, or public catalogue."
func (s *Server) handleCreateSongInSongbook(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanAccessSongbook(r.Context(), p.ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no access to this songbook")
		return
	}
	sb, err := s.store.GetSongbook(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	var req struct {
		Title, Artist string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	ownerKind, ownerID := store.OwnerPerson, sb.OwnerPersonID
	if sb.OwnerKind == "band" {
		ownerKind, ownerID = store.OwnerBand, sb.OwnerBandID
	}
	variantID, err := s.store.CreateVariant(r.Context(), ownerKind, ownerID, req.Title, req.Artist, p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"variantId": variantID})
}

// handleCreateCatalogSong creates a brand-new, public-catalogue variant
// (visibility='global', no owner) — the "public catalogue" option at
// creation time. Any logged-in person may do this (same as the old
// CreateSong's unrestricted "global" default); the result isn't added to
// any songbook automatically — add it to one explicitly via
// handleAddSongbookEntry, same as any other catalogue song.
func (s *Server) handleCreateCatalogSong(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title, Artist string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	variantID, err := s.store.CreateVariant(r.Context(), store.OwnerCatalog, 0, req.Title, req.Artist, actorID(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"variantId": variantID})
}

// handleAddSongbookEntry adds an existing (catalogue, or anyone's
// visible) variant to songbookID, by reference — "can add songs from the
// global catalog into (one of) their own song book."
func (s *Server) handleAddSongbookEntry(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanAccessSongbook(r.Context(), p.ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no access to this songbook")
		return
	}
	var req struct {
		VariantID int64 `json:"variantId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VariantID == 0 {
		httpError(w, http.StatusBadRequest, "missing variantId")
		return
	}
	if canView, err := s.store.CanViewVariant(r.Context(), p.ID, req.VariantID); err != nil || !canView {
		httpError(w, http.StatusForbidden, "can't see this song")
		return
	}
	if err := s.store.AddToSongbook(r.Context(), id, req.VariantID, p.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"added": true})
}

// handleRemoveSongbookEntry removes a variant pointer from songbookID —
// never deletes the variant itself.
func (s *Server) handleRemoveSongbookEntry(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	variantID, err := pathID(r, "variantID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanAccessSongbook(r.Context(), p.ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no access to this songbook")
		return
	}
	if err := s.store.RemoveFromSongbook(r.Context(), id, variantID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// handleDeleteSong permanently deletes a variant (not just a songbook
// pointer to it) — gated at the router by requireVariantRole("editor",
// ...), same bar as editing it.
func (s *Server) handleDeleteSong(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteVariant(id); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleRedoSong is the destructive "redo this song from scratch" action:
// resets an existing variant's body back to the blank template a brand-
// new song starts from — same id, same row, same revision history, but
// every bit of content discarded.
func (s *Server) handleRedoSong(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	text, newVersion, err := s.store.RedoVariant(id, req.Title, req.Version, actorID(r))
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
