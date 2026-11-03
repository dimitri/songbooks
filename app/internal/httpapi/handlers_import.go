package httpapi

import (
	"encoding/json"
	"net/http"

	"app/internal/importer"
)

type importRequest struct {
	URL string `json:"url"`
}

type importLyricsResponse struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

func (s *Server) handleImportLyrics(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		httpError(w, http.StatusBadRequest, "missing url")
		return
	}
	text, source, err := importer.FetchLyrics(req.URL)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, importLyricsResponse{Text: text, Source: source})
}

// handleImportLyricsFindFetch resolves title+artist to a real Genius song
// URL via importer.SearchGenius (the sanctioned API, not a search-page
// scrape), then runs the SAME fetch path handleImportLyrics already uses —
// one click lands a draft in Review instead of "open a new tab, hunt for
// the real link, copy it, paste it back" (the gap the user flagged). Falls
// back to a distinct "not configured" response when no API token is set so
// the editor can show a clear message rather than a raw error.
func (s *Server) handleImportLyricsFindFetch(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	artist := r.URL.Query().Get("artist")
	if title == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	songURL, err := importer.SearchGenius(title, artist)
	if err != nil {
		if err == importer.ErrGeniusNotConfigured {
			httpError(w, http.StatusNotImplemented, err.Error())
			return
		}
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	text, source, err := importer.FetchLyrics(songURL)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Text   string `json:"text"`
		Source string `json:"source"`
		URL    string `json:"url"`
	}{Text: text, Source: source, URL: songURL})
}

func (s *Server) handleImportChords(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		httpError(w, http.StatusBadRequest, "missing url")
		return
	}
	text, err := importer.FetchUGTab(req.URL)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}
