package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	"app/internal/store"
)

// maxGPSourceUpload mirrors maxRawAudioUpload (audio.go) — a real GP
// file's own embedded backing track can already be tens of MB, and the
// file as a whole is never transcoded/shrunk the way audio is, so it
// gets the same generous ceiling as a raw pre-compression audio upload.
const maxGPSourceUpload = 250 << 20

// handlePutSongGPFile is PUT /api/songs/{id}/gp-file — called by the GP
// import wizard (gp-import.js) alongside its existing audio/scores
// calls, so the ORIGINAL file a community member can re-download is
// never lost to "we only kept what we extracted from it."
func (s *Server) handlePutSongGPFile(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := r.ParseMultipartForm(maxGPSourceUpload); err != nil {
		httpError(w, http.StatusBadRequest, "file too large or not multipart/form-data: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxGPSourceUpload+1))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(data) > maxGPSourceUpload {
		httpError(w, http.StatusRequestEntityTooLarge, "file exceeds 250MB")
		return
	}
	filename := filepath.Base(header.Filename)
	if filename == "" || filename == "." {
		filename = "import.gp"
	}
	sum := sha256.Sum256(data)
	if err := s.store.SetSongGPSourceFile(r.Context(), variantID, filename, "application/octet-stream", data, hex.EncodeToString(sum[:]), actorID(r)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"filename": filename, "byteSize": len(data)})
}

// handleGetSongGPFile is GET /song/{id}/gp-file — a direct download of
// the original Guitar Pro file, Content-Disposition: attachment so it
// saves with its own real filename/extension instead of opening inline.
func (s *Server) handleGetSongGPFile(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	data, contentType, filename, err := s.store.GetSongGPSourceFileData(r.Context(), variantID)
	if err != nil {
		if err == store.ErrNotFound {
			http.NotFound(w, r)
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	serveGPFile(w, r, data, contentType, filename)
}

// handleGetSongGPFileVersion is GET /song/{id}/gp-file/versions/{vid}: an
// earlier Guitar Pro file of the song, kept when a newer one replaced it.
func (s *Server) handleGetSongGPFileVersion(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	versionID, err := pathID(r, "vid")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid version id")
		return
	}
	data, contentType, filename, err := s.store.GetSongGPSourceFileVersionData(r.Context(), variantID, versionID)
	if err != nil {
		if err == store.ErrNotFound {
			http.NotFound(w, r)
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	serveGPFile(w, r, data, contentType, filename)
}

func serveGPFile(w http.ResponseWriter, r *http.Request, data []byte, contentType, filename string) {
	w.Header().Set("Content-Type", contentType)
	// FormatMediaType quotes or RFC 2231-encodes the name as needed (an
	// accented title, a quote in it).
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	http.ServeContent(w, r, filename, time.Time{}, bytes.NewReader(data))
}
