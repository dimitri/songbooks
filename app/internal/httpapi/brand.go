package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"app/internal/brand"
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
