package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"app/internal/brand"
	"app/internal/store"
)

// handleSetBrand is POST /api/communities/{id}/brand and POST
// /api/bands/{id}/brand: the community's or band's look (internal/brand),
// replaced as a whole. Invalid fields are dropped, not rejected (see
// brand.Clean); the saved brand comes back.
func (s *Server) handleSetBrand(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var b brand.Brand
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&b); err != nil {
			httpError(w, http.StatusBadRequest, "invalid brand")
			return
		}
		b = brand.Clean(b)
		if err := s.store.SetBrand(r.Context(), kind, id, b); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, b)
	}
}

// handleGetBrand is GET /api/communities/{id}/brand and GET
// /api/bands/{id}/brand: the stored brand, for the settings form.
func (s *Server) handleGetBrand(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid id")
			return
		}
		b, _, err := s.store.GetBrand(r.Context(), kind, id)
		if err != nil {
			httpError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, b)
	}
}

// handleBrandFromSongs is the look a band's or community's songs make
// (brand.Derive over their covers, docs/covers.md), for the brand form to
// start from: the brand, and how many of the songs have a cover.
func (s *Server) handleBrandFromSongs(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid id")
			return
		}
		songs, err := s.store.OwnerCovers(r.Context(), kind, id)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		look := store.SongbookLook(songs)
		writeJSON(w, http.StatusOK, map[string]any{
			"brand": look.Brand, "mood": look.Mood, "genres": look.Genres,
			"covers": look.Found, "songs": look.Songs,
		})
	}
}
