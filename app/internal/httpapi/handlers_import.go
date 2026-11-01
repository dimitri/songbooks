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
