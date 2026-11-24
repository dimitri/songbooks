package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

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
		// The uploaded logo isn't the form's to set (handlePutBrandLogo).
		b = brand.Clean(b)
		b.LogoImage = ""
		// A band's tagline and website are The band section's
		// (handleSetBrandWords, edit_band), not its Look's: the stored
		// ones stay.
		if kind == "band" {
			stored, _, err := s.store.GetBrand(r.Context(), kind, id)
			if err != nil {
				httpError(w, http.StatusNotFound, "not found")
				return
			}
			b.Tagline, b.Website = stored.Tagline, stored.Website
		}
		if err := s.store.SetBrand(r.Context(), kind, id, b); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if saved, _, err := s.store.GetBrand(r.Context(), kind, id); err == nil {
			b = saved
		}
		writeJSON(w, http.StatusOK, b)
	}
}

// handleSetBrandWords is POST /api/bands/{id}/brand/words {"tagline",
// "website"}: the words of a brand (the line under the name, the
// website), changed alone, the look kept as it is — The band section of
// a band's Settings. A website that isn't an https address is refused,
// not dropped. The saved brand comes back.
func (s *Server) handleSetBrandWords(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var req struct {
			Tagline string `json:"tagline"`
			Website string `json:"website"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		b, _, err := s.store.GetBrand(r.Context(), kind, id)
		if err != nil {
			httpError(w, http.StatusNotFound, "not found")
			return
		}
		b.Tagline, b.Website = req.Tagline, strings.TrimSpace(req.Website)
		b = brand.Clean(b)
		if b.Website == "" && strings.TrimSpace(req.Website) != "" {
			httpError(w, http.StatusBadRequest, "the website must be an address that starts with https://")
			return
		}
		b.LogoImage = ""
		if err := s.store.SetBrand(r.Context(), kind, id, b); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if saved, _, err := s.store.GetBrand(r.Context(), kind, id); err == nil {
			b = saved
		}
		writeJSON(w, http.StatusOK, b)
	}
}

// handlePutBrandLogo is PUT /api/communities/{id}/brand/logo and PUT
// /api/bands/{id}/brand/logo: an image uploaded as the logo (multipart,
// field "file"), checked and remade by brand.PrepareLogo and kept in the
// database. The brand comes back, its logoImage the new address. Who may
// save the brand may upload it (the routes' permissions are the brand
// form's).
func (s *Server) handlePutBrandLogo(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid id")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, brand.MaxLogoUpload+64<<10)
		if err := r.ParseMultipartForm(brand.MaxLogoUpload + 64<<10); err != nil {
			httpError(w, http.StatusRequestEntityTooLarge, "the image is larger than 2 MB, or not sent as a file")
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			httpError(w, http.StatusBadRequest, "missing file field")
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, brand.MaxLogoUpload+1))
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		logo, err := brand.PrepareLogo(data)
		switch {
		case errors.Is(err, brand.ErrLogoTooLarge):
			httpError(w, http.StatusRequestEntityTooLarge, err.Error())
			return
		case errors.Is(err, brand.ErrLogoFormat), errors.Is(err, brand.ErrLogoSVG):
			httpError(w, http.StatusUnsupportedMediaType, err.Error())
			return
		case err != nil:
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var by int64
		if p := personFromContext(r); p != nil {
			by = p.ID
		}
		if err := s.store.SetBrandLogo(r.Context(), kind, id, logo, by); errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not found")
			return
		} else if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		b, _, err := s.store.GetBrand(r.Context(), kind, id)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, b)
	}
}

// handleDeleteBrandLogo is DELETE /api/communities/{id}/brand/logo and
// DELETE /api/bands/{id}/brand/logo: the uploaded logo removed (an https
// address in the form, if any, shows again). The brand comes back.
func (s *Server) handleDeleteBrandLogo(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if err := s.store.DeleteBrandLogo(r.Context(), kind, id); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
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

// handleGetBrandLogo is GET /brand-logos/{kind}/{id}/{file}, file being
// the logo's hash and extension (brand.LogoPath). It needs no login: a
// brand's logo shows on its public pages too, and its address can't be
// guessed. As the address changes with the image, it is cached for a
// year; an address of a replaced logo is not found.
func (s *Server) handleGetBrandLogo(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || (kind != "community" && kind != "band") {
		http.NotFound(w, r)
		return
	}
	l, err := s.store.GetBrandLogo(r.Context(), kind, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if path.Base(brand.LogoPath(kind, id, l.SHA256, l.ContentType)) != r.PathValue("file") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", l.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+l.SHA256+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", l.CreatedAt, bytes.NewReader(l.Data))
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
