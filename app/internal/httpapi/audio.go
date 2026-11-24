package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"

	"app/internal/audiotranscode"
	"app/internal/songimport"
	"app/internal/store"
	"app/internal/ytaudio"
)

// maxAudioUpload is a sane size ceiling for what's actually STORED — a
// reference MP3 for one song, not an album; 50MB covers even a long,
// high-bitrate track with room to spare. yt-dlp's own extraction isn't
// bounded by this (it transcodes before the app ever sees the bytes),
// but the download itself is bounded by ytaudio.Timeout instead.
const maxAudioUpload = 50 << 20

// maxRawAudioUpload is the ceiling for what's accepted FROM the client,
// before audiotranscode.ToMP3 gets a chance to compress it down toward
// maxAudioUpload — a direct upload can be an uncompressed WAV (a real
// Guitar Pro file's own embedded backing track came in at 67MB this
// session), not already an MP3 the way the YouTube-download path always
// is, so this ceiling has to cover raw PCM for a full-length song, not
// just a compressed one.
const maxRawAudioUpload = 250 << 20

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
	Kind          string          `json:"kind,omitempty"` // "full", "backing", "" unknown
	SourceURL     string          `json:"sourceUrl,omitempty"`
	Markers       []jsonAudioMark `json:"markers,omitempty"`
	BarSyncPoints []jsonGPBarSync `json:"barSyncPoints,omitempty"`
	// Its parts (store.Stems), split in the background (cmd/stemsd):
	// those there are, and where the split is ("queued", "running",
	// "done", "failed").
	Stems      []string `json:"stems,omitempty"`
	StemStatus string   `json:"stemStatus,omitempty"`
	// AnalysisStatus: where the recording's own analysis (beats, bars,
	// chords: migration 0106) is, "done" once there's one.
	AnalysisStatus string `json:"analysisStatus,omitempty"`
	// SyncOrigin: "" when nothing is synced, "auto" when any of the sync
	// was placed by a program and not checked yet, "set" otherwise.
	SyncOrigin string `json:"syncOrigin,omitempty"`
}

type jsonAudioMark struct {
	SectionIndex int     `json:"sectionIndex"`
	TimeSeconds  float64 `json:"timeSeconds"`
	Origin       string  `json:"origin,omitempty"` // "auto" or "set"
}

// mergeOrigin folds one marker's origin into a recording's: any
// estimate makes the whole sync estimated.
func mergeOrigin(cur, o string) string {
	if cur == store.SyncAuto || o == store.SyncAuto {
		return store.SyncAuto
	}
	return store.SyncSet
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
		cm := audioCategoryMeta{Category: string(a.Category), ByteSize: a.ByteSize, SourceURL: a.SourceURL, Kind: a.Kind}
		if ss, err := s.store.GetStemStatus(r.Context(), a.ID); err == nil {
			cm.Stems, cm.StemStatus = ss.Stems, ss.Status
		}
		for _, m := range markers {
			cm.Markers = append(cm.Markers, jsonAudioMark{SectionIndex: m.SectionIndex, TimeSeconds: m.TimeSeconds, Origin: m.Origin})
			cm.SyncOrigin = mergeOrigin(cm.SyncOrigin, m.Origin)
		}
		if as, err := s.store.GetAnalysisState(r.Context(), a.ID); err == nil {
			cm.AnalysisStatus = as
		}
		// Every bar's time: a Guitar Pro file's own, or the recording's
		// analysis accepted (any category).
		{
			points, err := s.store.ListGPBarSyncPoints(r.Context(), a.ID)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, p := range points {
				cm.BarSyncPoints = append(cm.BarSyncPoints, jsonGPBarSync{BarIndex: p.BarIndex, TimeSeconds: p.TimeSeconds})
				cm.SyncOrigin = mergeOrigin(cm.SyncOrigin, p.Origin)
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
	// ?stem=vocals: one of its parts.
	if stem := r.URL.Query().Get("stem"); stem != "" {
		if !slices.Contains(store.Stems, stem) {
			http.NotFound(w, r)
			return
		}
		data, contentType, at, err := s.store.GetStemData(r.Context(), meta.ID, stem)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeContent(w, r, strconv.FormatInt(variantID, 10)+"-"+string(category)+"-"+stem+".mp3", at, bytes.NewReader(data))
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

// handlePostSongAudioStems is POST /api/songs/{id}/audio/{category}/stems:
// split the recording again (one that failed, or after a better model).
func (s *Server) handlePostSongAudioStems(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	category, ok := parseAudioCategory(r.PathValue("category"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	meta, err := s.store.GetSongAudioMeta(r.Context(), variantID, category)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.QueueStemJob(r.Context(), meta.ID); err != nil {
		if errors.Is(err, store.ErrNotSplit) {
			httpError(w, http.StatusConflict, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"stemStatus": "queued"})
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
	if err := r.ParseMultipartForm(maxRawAudioUpload); err != nil {
		httpError(w, http.StatusBadRequest, "file too large or not multipart/form-data: "+err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRawAudioUpload+1))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(data) > maxRawAudioUpload {
		httpError(w, http.StatusRequestEntityTooLarge, "file exceeds 250MB")
		return
	}
	// A GP import's embedded backing track routinely arrives as
	// uncompressed WAV (see maxRawAudioUpload's own doc comment); a
	// direct band-recording upload could too. Compress it to MP3 before
	// it ever reaches storeAudioBytes/Postgres — no reason to keep an
	// uncompressed reference track around at 10x the size for no
	// audible benefit on a practice/reference recording.
	if audiotranscode.NeedsTranscode(data) {
		mp3, err := audiotranscode.ToMP3(r.Context(), data)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "couldn't compress this audio: "+err.Error())
			return
		}
		data = mp3
	}
	if len(data) > maxAudioUpload {
		httpError(w, http.StatusRequestEntityTooLarge, "file exceeds 50MB even after compression")
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
		// "auto" from the Mixer's Auto-align; a Guitar Pro file's own
		// sync, or a person's, is "set" (the default).
		Origin string `json:"origin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	points := make([]store.GPBarSyncPoint, len(req.Points))
	for i, p := range req.Points {
		points[i] = store.GPBarSyncPoint{BarIndex: p.BarIndex, TimeSeconds: p.TimeSeconds, Origin: req.Origin}
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

// audioMetaFor resolves {id} and {category} to the recording, writing
// the error itself when there isn't one.
func (s *Server) audioMetaFor(w http.ResponseWriter, r *http.Request) (*store.SongAudio, bool) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return nil, false
	}
	category, ok := parseAudioCategory(r.PathValue("category"))
	if !ok {
		httpError(w, http.StatusBadRequest, `category must be "youtube", "band", or "gp"`)
		return nil, false
	}
	meta, err := s.store.GetSongAudioMeta(r.Context(), variantID, category)
	if err != nil {
		httpError(w, http.StatusNotFound, "no audio for this song/category yet")
		return nil, false
	}
	return meta, true
}

// handlePutAudioMarkers is PUT /api/songs/{id}/audio/{category}/markers:
// the recording's whole sync in one request (the Sync sheet's Save, the
// Mixer's Auto-align), body {"markers": [{"sectionIndex", "timeSeconds",
// "origin": "auto"|"set"}]}. Sections left out lose their marker. The
// sync it replaces goes to the history (GET .../markers/history).
func (s *Server) handlePutAudioMarkers(w http.ResponseWriter, r *http.Request) {
	meta, ok := s.audioMetaFor(w, r)
	if !ok {
		return
	}
	var req struct {
		Markers []jsonAudioMark `json:"markers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	seen := map[int]bool{}
	markers := make([]store.AudioMarker, 0, len(req.Markers))
	for _, m := range req.Markers {
		if m.SectionIndex < 0 || seen[m.SectionIndex] || m.TimeSeconds != m.TimeSeconds || m.TimeSeconds < -60 || m.TimeSeconds > 6*3600 {
			httpError(w, http.StatusBadRequest, "invalid marker")
			return
		}
		if m.Origin != "" && m.Origin != store.SyncAuto && m.Origin != store.SyncSet {
			httpError(w, http.StatusBadRequest, `origin must be "auto" or "set"`)
			return
		}
		seen[m.SectionIndex] = true
		markers = append(markers, store.AudioMarker{SectionIndex: m.SectionIndex, TimeSeconds: m.TimeSeconds, Origin: m.Origin})
	}
	// The sections play in their order: a later one starting before an
	// earlier one runs the bar grid backwards.
	if !markersInOrder(markers) {
		httpError(w, http.StatusBadRequest, "the sections' starts are out of order")
		return
	}
	points, err := s.reconcileBarPoints(r, meta, markers)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.ReplaceSync(r.Context(), meta.ID, markers, actorID(r), points); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(markers)})
}

// markersInOrder: by section, the starts never go back in time (two at
// the same time are allowed: a section with no bars).
func markersInOrder(markers []store.AudioMarker) bool {
	ms := slices.Clone(markers)
	slices.SortFunc(ms, func(a, b store.AudioMarker) int { return a.SectionIndex - b.SectionIndex })
	for i := 1; i < len(ms); i++ {
		if ms[i].TimeSeconds < ms[i-1].TimeSeconds {
			return false
		}
	}
	return true
}

// handleGetAudioMarkersHistory is GET /api/songs/{id}/audio/{category}/markers/history:
// the syncs earlier saves replaced, newest first, to take one back.
func (s *Server) handleGetAudioMarkersHistory(w http.ResponseWriter, r *http.Request) {
	meta, ok := s.audioMetaFor(w, r)
	if !ok {
		return
	}
	list, err := s.store.ListSyncHistory(r.Context(), meta.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type entry struct {
		ID      int64           `json:"id"`
		SavedAt string          `json:"savedAt"`
		SavedBy string          `json:"savedBy,omitempty"`
		Markers []jsonAudioMark `json:"markers"`
	}
	out := make([]entry, 0, len(list))
	for _, e := range list {
		je := entry{ID: e.ID, SavedAt: e.SavedAt.UTC().Format("2006-01-02T15:04:05Z"), SavedBy: e.SavedBy, Markers: []jsonAudioMark{}}
		for _, m := range e.Markers {
			je.Markers = append(je.Markers, jsonAudioMark{SectionIndex: m.SectionIndex, TimeSeconds: m.TimeSeconds, Origin: m.Origin})
		}
		out = append(out, je)
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": out})
}

// handleGetAudioVoice is GET /api/songs/{id}/audio/{category}/voice: when
// the singer sings in the recording (its phrases, from the vocals part),
// for the Sync sheet's snapping and the lyrics view's lines. 404 until
// the recording is split.
func (s *Server) handleGetAudioVoice(w http.ResponseWriter, r *http.Request) {
	meta, ok := s.audioMetaFor(w, r)
	if !ok {
		return
	}
	c, err := songimport.Voice(r.Context(), s.store, meta.VariantID, meta.Category)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, songimport.ErrNoVoice) {
			code = http.StatusNotFound
		}
		httpError(w, code, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	writeJSON(w, http.StatusOK, c)
}

// handlePutSongAudioKind is PUT /api/songs/{id}/audio/{category}/kind,
// body {"kind": "full" | "backing" | ""}: what this reference recording
// is — the whole song, or a backing track with the practised part left
// out (see migration 0063).
func (s *Server) handlePutSongAudioKind(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	cat, ok := parseAudioCategory(r.PathValue("category"))
	if !ok {
		httpError(w, http.StatusBadRequest, "unknown audio category")
		return
	}
	var req struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Kind != "" && req.Kind != "full" && req.Kind != "backing" {
		httpError(w, http.StatusBadRequest, `kind must be "full", "backing" or ""`)
		return
	}
	if err := s.store.SetSongAudioKind(r.Context(), variantID, cat, req.Kind); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
