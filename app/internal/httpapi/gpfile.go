package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	"app/internal/gpfacts"
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
	facts, ok := factsField(w, r.FormValue("facts"))
	if !ok {
		return
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if err := s.store.SetSongGPSourceFile(r.Context(), variantID, filename, "application/octet-stream", data, sha, actorID(r), facts); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"filename": filename, "byteSize": len(data), "sha256": sha})
}

// factsField checks a file's facts as sent by gp-facts.js: "" is none,
// anything else must decode. ok is false once an error is written.
func factsField(w http.ResponseWriter, raw string) ([]byte, bool) {
	if raw == "" {
		return nil, true
	}
	if len(raw) > 2<<20 {
		httpError(w, http.StatusBadRequest, "facts too large")
		return nil, false
	}
	if _, err := gpfacts.Parse([]byte(raw)); err != nil {
		httpError(w, http.StatusBadRequest, "bad facts: "+err.Error())
		return nil, false
	}
	return []byte(raw), true
}

// handlePutSongGPFileFacts is PUT /api/songs/{id}/gp-file/facts
// {sha256, facts}: the facts a browser read from a stored file that had
// none yet (files stored before facts existed). Ignored when the file has
// changed since.
func (s *Server) handlePutSongGPFileFacts(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		SHA256 string          `json:"sha256"`
		Facts  json.RawMessage `json:"facts"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	facts, ok := factsField(w, string(req.Facts))
	if !ok {
		return
	}
	stored, err := s.store.SetSongGPSourceFileFacts(r.Context(), variantID, req.SHA256, facts)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stored": stored})
}

// gpTrackProgram is one track's instrument as the song's editors chose
// it (store.SongGPSourceFile.TrackPrograms).
type gpTrackProgram struct {
	Track   int    `json:"track"`
	Name    string `json:"name"`
	Program int    `json:"program"`
}

// handlePutSongGPFileInstruments is PUT /api/songs/{id}/gp-file/instruments
// {tracks: [{track, name, program}]}: the instrument each track of the
// song's file plays with, for everyone (editors; an empty list goes back
// to the file's own). gp-instruments.js.
func (s *Server) handlePutSongGPFileInstruments(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Tracks []gpTrackProgram `json:"tracks"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(req.Tracks) > 64 {
		httpError(w, http.StatusBadRequest, "too many tracks")
		return
	}
	seen := map[int]bool{}
	out := make([]gpTrackProgram, 0, len(req.Tracks))
	for _, t := range req.Tracks {
		if t.Track < 0 || t.Track >= 64 || t.Program < 0 || t.Program > 127 || len(t.Name) > 200 || seen[t.Track] {
			httpError(w, http.StatusBadRequest, "invalid track")
			return
		}
		seen[t.Track] = true
		out = append(out, t)
	}
	var raw []byte
	if len(out) > 0 {
		raw, _ = json.Marshal(out)
	}
	ok, err := s.store.SetSongGPTrackPrograms(r.Context(), variantID, raw)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tracks": out})
}

// gpTrackProgramsJSON is a file's chosen instruments for a page or the
// Mixer: "[]" when none.
func gpTrackProgramsJSON(meta *store.SongGPSourceFile) json.RawMessage {
	if meta == nil || len(meta.TrackPrograms) == 0 {
		return json.RawMessage("[]")
	}
	return json.RawMessage(meta.TrackPrograms)
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
	// The song page loads the file to draw its "gp[...]" bars: let the
	// browser keep it and only ask whether it changed.
	sum := sha256.Sum256(data)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:16])+`"`)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Type", contentType)
	// FormatMediaType quotes or RFC 2231-encodes the name as needed (an
	// accented title, a quote in it).
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	http.ServeContent(w, r, filename, time.Time{}, bytes.NewReader(data))
}
