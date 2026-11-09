package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// parseAudioCategory validates the "category" path value against the
// two allowed categories (store.AudioCategory's own doc comment: "a
// personal choice of reference version (yt video, band recording: two
// categories only)") — never an open-ended string straight from the URL.
func parseAudioCategory(s string) (store.AudioCategory, bool) {
	switch store.AudioCategory(s) {
	case store.AudioYouTube, store.AudioBand, store.AudioGP:
		return store.AudioCategory(s), true
	default:
		return "", false
	}
}

type audioCategoryMeta struct {
	Category      string          `json:"category"`
	ByteSize      int64           `json:"byteSize"`
	SourceURL     string          `json:"sourceUrl,omitempty"`
	Markers       []jsonAudioMark `json:"markers,omitempty"`
	BarSyncPoints []jsonGPBarSync `json:"barSyncPoints,omitempty"`
}

type jsonAudioMark struct {
	SectionIndex int     `json:"sectionIndex"`
	TimeSeconds  float64 `json:"timeSeconds"`
}

// jsonGPBarSync is one GPBarSyncPoint on the wire — only ever present on
// the "gp" category (see store.GPBarSyncPoint's doc comment), used by
// the player to let a click on any rendered bar seek straight to it.
type jsonGPBarSync struct {
	BarIndex    int     `json:"barIndex"`
	TimeSeconds float64 `json:"timeSeconds"`
}

// handleGetSongAudioMeta is GET /api/songs/{book}/{stem}/audio — every
// category this song has reference audio for (0, 1, 2, or 3 entries),
// each with its markers (and, for "gp", its bar sync points), without
// ever loading the audio bytes themselves.
func (s *Server) handleGetSongAudioMeta(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	list, err := s.store.ListSongAudio(r.Context(), variantID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]audioCategoryMeta, 0, len(list))
	for _, a := range list {
		markers, err := s.store.ListAudioMarkers(r.Context(), a.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cm := audioCategoryMeta{Category: string(a.Category), ByteSize: a.ByteSize, SourceURL: a.SourceURL}
		for _, m := range markers {
			cm.Markers = append(cm.Markers, jsonAudioMark{SectionIndex: m.SectionIndex, TimeSeconds: m.TimeSeconds})
		}
		if a.Category == store.AudioGP {
			points, err := s.store.ListGPBarSyncPoints(r.Context(), a.ID)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, p := range points {
				cm.BarSyncPoints = append(cm.BarSyncPoints, jsonGPBarSync{BarIndex: p.BarIndex, TimeSeconds: p.TimeSeconds})
			}
		}
		out = append(out, cm)
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": out})
}

// handleGetSongAudioData is GET /b/{book}/{stem}/audio/{category} — note
// no ".mp3" suffix: Go 1.22+ ServeMux rejects a wildcard segment with a
// literal suffix ("bad wildcard segment") — serving at the Content-Type
// (audio/mpeg) alone instead, which every browser respects for <audio src>.
// serves the stored bytes with Range-request support (http.ServeContent
// implements it correctly already, so the player's seek/scrub never
// re-downloads the whole file).
func (s *Server) handleGetSongAudioData(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	category, ok := parseAudioCategory(r.URL.Query().Get("category"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	meta, err := s.store.GetSongAudioMeta(r.Context(), variantID, category)
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
	http.ServeContent(w, r, strconv.FormatInt(variantID, 10)+"-"+string(category)+".mp3", meta.CreatedAt, bytes.NewReader(data))
}

// handlePutSongAudio is PUT /api/songs/{book}/{stem}/audio/{category} — a
// direct file upload for that one category.
func (s *Server) handlePutSongAudio(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	category, ok := parseAudioCategory(r.PathValue("category"))
	if !ok {
		httpError(w, http.StatusBadRequest, `category must be "youtube", "band", or "gp"`)
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
	s.storeAudioBytes(w, r, variantID, category, data, "audio/mpeg", "")
}

// handlePostSongAudioYouTube is POST /api/songs/{book}/{stem}/audio/youtube
// — the yt-dlp convenience (app/docs/mp3-reference-audio.md §1): runs
// yt-dlp server-side against the given URL, extracts+transcodes to MP3,
// and stores the result under the "youtube" category — the download
// convenience only ever fills that one category, never "band", since the
// two exist specifically to distinguish a YouTube source from the band's
// own recording.
func (s *Server) handlePostSongAudioYouTube(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
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
	s.storeAudioBytes(w, r, variantID, store.AudioYouTube, data, "audio/mpeg", req.URL)
}

// storeAudioBytes is the shared tail of both upload paths: hash, store,
// respond with the same meta shape handleGetSongAudioMeta returns.
func (s *Server) storeAudioBytes(w http.ResponseWriter, r *http.Request, variantID int64, category store.AudioCategory, data []byte, contentType, sourceURL string) {
	sum := sha256.Sum256(data)
	id, err := s.store.SetSongAudio(r.Context(), variantID, category, data, contentType, hex.EncodeToString(sum[:]), sourceURL, actorID(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "category": category, "byteSize": len(data)})
}

func (s *Server) handleDeleteSongAudio(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	category, ok := parseAudioCategory(r.PathValue("category"))
	if !ok {
		httpError(w, http.StatusBadRequest, `category must be "youtube", "band", or "gp"`)
		return
	}
	if err := s.store.DeleteSongAudio(r.Context(), variantID, category); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePutAudioMarker is PUT /api/songs/{book}/{stem}/audio/{category}/markers/{sectionIndex}
// — "set marker here" for one section of one category, body {"timeSeconds": N}.
func (s *Server) handlePutAudioMarker(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	category, ok := parseAudioCategory(r.PathValue("category"))
	if !ok {
		httpError(w, http.StatusBadRequest, `category must be "youtube", "band", or "gp"`)
		return
	}
	sectionIndex, err := strconv.Atoi(r.PathValue("sectionIndex"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	meta, err := s.store.GetSongAudioMeta(r.Context(), variantID, category)
	if err != nil {
		httpError(w, http.StatusNotFound, "no audio for this song/category yet")
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

// handlePutGPBarSyncPoints is PUT /api/songs/{id}/audio/gp/sync-points —
// replaces the "gp" category's entire set of per-bar sync points in one
// call, body {"points": [{"barIndex": N, "timeSeconds": N}, ...]}. Always
// written as a whole batch (one GP import), never point-edited — see
// Store.SetGPBarSyncPoints' own doc comment.
func (s *Server) handlePutGPBarSyncPoints(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	meta, err := s.store.GetSongAudioMeta(r.Context(), variantID, store.AudioGP)
	if err != nil {
		httpError(w, http.StatusNotFound, "no gp audio for this song yet — upload it first")
		return
	}
	var req struct {
		Points []struct {
			BarIndex    int     `json:"barIndex"`
			TimeSeconds float64 `json:"timeSeconds"`
		} `json:"points"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	points := make([]store.GPBarSyncPoint, len(req.Points))
	for i, p := range req.Points {
		points[i] = store.GPBarSyncPoint{BarIndex: p.BarIndex, TimeSeconds: p.TimeSeconds}
	}
	if err := s.store.SetGPBarSyncPoints(r.Context(), meta.ID, points); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(points)})
}

func (s *Server) handleDeleteAudioMarker(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	category, ok := parseAudioCategory(r.PathValue("category"))
	if !ok {
		httpError(w, http.StatusBadRequest, `category must be "youtube", "band", or "gp"`)
		return
	}
	sectionIndex, err := strconv.Atoi(r.PathValue("sectionIndex"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	meta, err := s.store.GetSongAudioMeta(r.Context(), variantID, category)
	if err != nil {
		httpError(w, http.StatusNotFound, "no audio for this song/category yet")
		return
	}
	if err := s.store.DeleteAudioMarker(r.Context(), meta.ID, sectionIndex); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
