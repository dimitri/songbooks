package httpapi

import (
	"app/internal/i18n"
	"encoding/json"
	"net/http"
	"strings"

	"app/internal/importer"
	"app/internal/song"
	"app/internal/songimport"
	"app/internal/store"
	"app/internal/ytaudio"
)

// SyncSections is the chart's sections as the player numbers their bars
// (buildSections, the same code the chart renders with), for placing
// them on a recording. A bare repeat of a sung section ("[Chorus]" with
// nothing under it) counts as sung.
func SyncSections(sg *song.Song) []songimport.SyncSection {
	var out []songimport.SyncSection
	for _, sec := range buildSections(sg) {
		sung := len(sec.Lyrics) > 0
		if !sung && sec.Repeat {
			l := strings.ToLower(sec.Label)
			sung = !strings.Contains(l, "intro") && !strings.Contains(l, "outro") && !strings.Contains(l, "solo") && !strings.Contains(l, "instrument")
		}
		out = append(out, songimport.SyncSection{Index: sec.Index, Label: sec.Label, StartBar: sec.StartBar, BarCount: sec.BarCount, HasLyrics: sung})
	}
	return out
}

// handleImportYouTubeFind is GET /api/import/youtube/find?title=&artist=:
// candidate reference recordings, the artist's own upload of the
// studio version first (ytaudio.Search).
func (s *Server) handleImportYouTubeFind(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	if title == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	list, err := ytaudio.Search(r.Context(), title, r.URL.Query().Get("artist"))
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	if len(list) > 8 {
		list = list[:8]
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": list})
}

// handleImportCleanup is POST /api/import/cleanup {text, stripInlineChords}:
// importer.CleanSongText on a draft or on the song's source.
func (s *Server) handleImportCleanup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text              string `json:"text"`
		StripInlineChords bool   `json:"stripInlineChords"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		httpError(w, http.StatusBadRequest, "missing text")
		return
	}
	text, notes := importer.CleanSongText(req.Text, importer.CleanOptions{StripInlineChords: req.StripInlineChords})
	writeJSON(w, http.StatusOK, map[string]any{"text": text, "notes": notes})
}

// handleImportLyricsPlace is POST /api/import/lyrics/place: fetches one
// song's lyrics (source "ug", "url" or "text", see songimport.Stanzas)
// and places them in currentText's sections. Nothing is saved: the
// result goes back into the editor for the person to check.
func (s *Server) handleImportLyricsPlace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentText string `json:"currentText"`
		Source      string `json:"source"`
		Input       string `json:"input"`
		Title       string `json:"title"`
		Artist      string `json:"artist"`
		Replace     bool   `json:"replace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.CurrentText) == "" {
		httpError(w, http.StatusBadRequest, "currentText is required")
		return
	}
	if (req.Source == "" || req.Source == songimport.LyricsFromUG) && req.Title == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	res, err := songimport.PlaceLyrics(r.Context(), req.CurrentText, req.Source, req.Title, req.Artist, req.Input, req.Replace)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleImportVocal is GET /api/import/vocal?title=&artist=: the singer's
// draft, lyrics without chords and iReal Pro's grids (songimport.VocalDraft).
func (s *Server) handleImportVocal(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	if title == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	d, err := songimport.VocalDraft(r.Context(), s.store, title, r.URL.Query().Get("artist"))
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// syncRequest is the body of the analyze and sync calls: the chart text
// to place (the editor's, saved or not; the saved song's when empty).
type syncRequest struct {
	Text      string `json:"text"`
	Version   string `json:"version"`
	Overwrite bool   `json:"overwrite"`
}

func (s *Server) proposeSync(r *http.Request, variantID int64, category store.AudioCategory, text string) (*songimport.SyncProposal, int, error) {
	if strings.TrimSpace(text) == "" {
		_, _, _, body, err := s.store.VariantContent(r.Context(), variantID)
		if err != nil {
			return nil, http.StatusNotFound, err
		}
		text = body
	}
	a, ready, err := songimport.Recording(r.Context(), s.store, variantID, category)
	if err != nil {
		return nil, http.StatusNotFound, err
	}
	sg := song.Parse(text)
	irealSig, irealKey := "", ""
	if title := sg.Get("title"); title != "" {
		if _, info, err := songimport.FindIReal(r.Context(), s.store, title, sg.Get("artist")); err == nil {
			irealSig, irealKey = info.TimeSig, info.Key
		}
	}
	p, err := songimport.ProposeSync(a, ready, text, SyncSections(sg), irealSig, irealKey)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}
	return p, http.StatusOK, nil
}

// handlePostSongAudioAnalyze is POST /api/songs/{id}/audio/{category}/analyze:
// the recording's tempo, metre and key, and where the chart's sections
// start in it — a proposal, nothing written.
func (s *Server) handlePostSongAudioAnalyze(w http.ResponseWriter, r *http.Request) {
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
	var req syncRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	p, code, err := s.proposeSync(r, variantID, category, req.Text)
	if err != nil {
		httpError(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handlePostSongAudioSync is POST /api/songs/{id}/audio/{category}/sync:
// analyses the SAVED song again (markers number its sections) and writes
// the proposal: tempo and time headers, every section's marker.
func (s *Server) handlePostSongAudioSync(w http.ResponseWriter, r *http.Request) {
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
	var req syncRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	p, code, err := s.proposeSync(r, variantID, category, "")
	if err != nil {
		httpError(w, code, err.Error())
		return
	}
	text, version, err := songimport.ApplySync(r.Context(), s.store, variantID, category, p, req.Version, req.Overwrite, actorID(r))
	if err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposal": p, "text": text, "version": version})
}

// handlePostKeyCheck is POST /api/import/keycheck {text, songId}: the
// chart's key against iReal Pro's and the song's recording's.
func (s *Server) handlePostKeyCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text   string `json:"text"`
		SongID int64  `json:"songId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		httpError(w, http.StatusBadRequest, "missing text")
		return
	}
	sg := song.Parse(req.Text)
	irealKey := ""
	if title := sg.Get("title"); title != "" {
		if _, info, err := songimport.FindIReal(r.Context(), s.store, title, sg.Get("artist")); err == nil {
			irealKey = info.Key
		}
	}
	audioKey, conf := "", 0.0
	if req.SongID > 0 {
		if ok, _ := s.store.CanEditVariant(r.Context(), actorID(r), req.SongID); ok {
			if a, _, err := songimport.Recording(r.Context(), s.store, req.SongID, store.AudioYouTube); err == nil {
				audioKey, conf = a.Key, a.KeyConfidence
			}
		}
	}
	writeJSON(w, http.StatusOK, songimport.CheckKey(req.Text, irealKey, audioKey, conf))
}

// handleGetImportAssess is GET /api/songs/{id}/import/assess: what the
// import wizard would do with the saved song (songimport.Assess) — new,
// improve or restart, and the steps worth doing. iReal Pro from its
// cache only; nothing fetched, nothing written.
func (s *Server) handleGetImportAssess(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	title, _, _, body, err := s.store.VariantContent(r.Context(), variantID)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	irealKey := ""
	if strings.TrimSpace(title) != "" {
		if cached, err := s.store.FindIRealSong(r.Context(), title); err == nil {
			irealKey = cached.Key
		}
	}
	var rec songimport.RecordingState
	if meta, err := s.store.GetSongAudioMeta(r.Context(), variantID, store.AudioYouTube); err == nil {
		rec.Has = true
		if st, err := s.store.GetStemStatus(r.Context(), meta.ID); err == nil {
			rec.StemStatus = st.Status
		}
		if m, err := s.store.ListAudioMarkers(r.Context(), meta.ID); err == nil {
			rec.Markers = len(m)
		}
	}
	writeJSON(w, http.StatusOK, songimport.Assess(body, rec, irealKey, func(f string, args ...any) string { return i18n.T(s.langOf(w), f, args...) }))
}

// handleImportMerge is POST /api/import/merge {currentText, draft, mode}:
// a fetched draft brought into the song's text. mode "merge" (the
// default) keeps the song's structure and adds what it lacks
// (importer.MergeDraft); "replace" takes the draft's sections under the
// song's header (importer.ReplaceDraft); "append" puts the draft below
// the song as it is. Nothing is saved.
func (s *Server) handleImportMerge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentText string `json:"currentText"`
		Draft       string `json:"draft"`
		Mode        string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Draft) == "" {
		httpError(w, http.StatusBadRequest, "missing draft")
		return
	}
	var text string
	var notes []string
	switch req.Mode {
	case "merge", "":
		text, notes = importer.MergeDraft(req.CurrentText, req.Draft)
	case "replace":
		text, notes = importer.ReplaceDraft(req.CurrentText, req.Draft)
	case "append":
		text = req.CurrentText + "\n\n" + req.Draft
		notes = []string{"Draft appended below the song"}
	default:
		httpError(w, http.StatusBadRequest, "mode must be merge, replace or append")
		return
	}
	if notes == nil {
		notes = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": text, "notes": notes})
}
