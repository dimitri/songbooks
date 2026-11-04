package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"app/internal/store"
	"app/internal/ytaudio"
)

// maxAudioUpload is a sane size ceiling for a direct file upload — a
// reference MP3 for one song, not an album; 50MB covers even a long,
// high-bitrate track with room to spare. yt-dlp's own extraction isn't
// bounded by this (it transcodes before the app ever sees the bytes),
// but the download itself is bounded by ytaudio.Timeout instead.
const maxAudioUpload = 50 << 20

type audioMeta struct {
	HasAudio  bool            `json:"hasAudio"`
	ByteSize  int64           `json:"byteSize,omitempty"`
	SourceURL string          `json:"sourceUrl,omitempty"`
	Markers   []jsonAudioMark `json:"markers,omitempty"`
}

type jsonAudioMark struct {
	SectionIndex int     `json:"sectionIndex"`
	TimeSeconds  float64 `json:"timeSeconds"`
}

// handleGetSongAudioMeta is GET /api/songs/{book}/{stem}/audio — whether
// this song has reference audio yet, its size, and its markers, without
// ever loading the audio bytes themselves (see GetSongAudioMeta's own
// doc comment).
func (s *Server) handleGetSongAudioMeta(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	variantID, _, err := s.store.VariantLookup(r.Context(), book, stem)
	if err != nil || variantID == 0 {
		writeJSON(w, http.StatusOK, audioMeta{})
		return
	}
	a, err := s.store.GetSongAudioMeta(r.Context(), variantID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, audioMeta{})
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	markers, err := s.store.ListAudioMarkers(r.Context(), a.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := audioMeta{HasAudio: true, ByteSize: a.ByteSize, SourceURL: a.SourceURL}
	for _, m := range markers {
		out.Markers = append(out.Markers, jsonAudioMark{SectionIndex: m.SectionIndex, TimeSeconds: m.TimeSeconds})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetSongAudioData is GET /b/{book}/{stem}/audio.mp3 — serves the
// stored bytes with Range-request support (http.ServeContent implements
// it correctly already, so the player's seek/scrub never re-downloads
// the whole file).
func (s *Server) handleGetSongAudioData(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	variantID, _, err := s.store.VariantLookup(r.Context(), book, stem)
	if err != nil || variantID == 0 {
		http.NotFound(w, r)
		return
	}
	meta, err := s.store.GetSongAudioMeta(r.Context(), variantID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, contentType, err := s.store.GetSongAudioData(r.Context(), meta.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, stem+".mp3", meta.CreatedAt, bytes.NewReader(data))
}

func (s *Server) handlePutSongAudio(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	variantID, _, err := s.store.VariantLookup(r.Context(), book, stem)
	if err != nil || variantID == 0 {
		httpError(w, http.StatusNotFound, "no such song")
		return
	}
	if err := r.ParseMultipartForm(maxAudioUpload); err != nil {
		httpError(w, http.StatusBadRequest, "file too large or not multipart/form-data: "+err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAudioUpload+1))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(data) > maxAudioUpload {
		httpError(w, http.StatusRequestEntityTooLarge, "file exceeds 50MB")
		return
	}
	s.storeAudioBytes(w, r, variantID, data, "audio/mpeg", "")
}

// handlePostSongAudioYouTube is POST /api/songs/{book}/{stem}/audio/youtube
// — the yt-dlp convenience (app/docs/mp3-reference-audio.md §1): runs
// yt-dlp server-side against the given URL, extracts+transcodes to MP3,
// and stores the result exactly like a direct upload would.
func (s *Server) handlePostSongAudioYouTube(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	variantID, _, err := s.store.VariantLookup(r.Context(), book, stem)
	if err != nil || variantID == 0 {
		httpError(w, http.StatusNotFound, "no such song")
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		httpError(w, http.StatusBadRequest, "missing url")
		return
	}
	data, err := ytaudio.Download(r.Context(), req.URL)
	if err != nil {
		httpError(w, http.StatusBadGateway, "download failed: "+err.Error())
		return
	}
	s.storeAudioBytes(w, r, variantID, data, "audio/mpeg", req.URL)
}

// storeAudioBytes is the shared tail of both upload paths: hash, store,
// respond with the same meta shape handleGetSongAudioMeta returns.
func (s *Server) storeAudioBytes(w http.ResponseWriter, r *http.Request, variantID int64, data []byte, contentType, sourceURL string) {
	sum := sha256.Sum256(data)
	id, err := s.store.SetSongAudio(r.Context(), variantID, data, contentType, hex.EncodeToString(sum[:]), sourceURL, actorID(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "byteSize": len(data)})
}

func (s *Server) handleDeleteSongAudio(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	variantID, _, err := s.store.VariantLookup(r.Context(), book, stem)
	if err != nil || variantID == 0 {
		httpError(w, http.StatusNotFound, "no such song")
		return
	}
	if err := s.store.DeleteSongAudio(r.Context(), variantID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePutAudioMarker is PUT /api/songs/{book}/{stem}/audio/markers/{sectionIndex}
// — "set marker here" for one section, body {"timeSeconds": N}.
func (s *Server) handlePutAudioMarker(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	sectionIndex, err := strconv.Atoi(r.PathValue("sectionIndex"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	variantID, _, err := s.store.VariantLookup(r.Context(), book, stem)
	if err != nil || variantID == 0 {
		httpError(w, http.StatusNotFound, "no such song")
		return
	}
	meta, err := s.store.GetSongAudioMeta(r.Context(), variantID)
	if err != nil {
		httpError(w, http.StatusNotFound, "no audio for this song yet")
		return
	}
	var req struct {
		TimeSeconds float64 `json:"timeSeconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetAudioMarker(r.Context(), meta.ID, sectionIndex, req.TimeSeconds); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteAudioMarker(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	sectionIndex, err := strconv.Atoi(r.PathValue("sectionIndex"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	variantID, _, err := s.store.VariantLookup(r.Context(), book, stem)
	if err != nil || variantID == 0 {
		httpError(w, http.StatusNotFound, "no such song")
		return
	}
	meta, err := s.store.GetSongAudioMeta(r.Context(), variantID)
	if err != nil {
		httpError(w, http.StatusNotFound, "no audio for this song yet")
		return
	}
	if err := s.store.DeleteAudioMarker(r.Context(), meta.ID, sectionIndex); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
