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

// ugSectionPreview is one parsed section as the import wizard's preview
// step needs it — plain strings, no song.Section/UGSong types exposed to
// the client, so the JS side never has to re-derive the grid/lyrics
// rendering rules.
type ugSectionPreview struct {
	Label  string `json:"label"`
	Grid   string `json:"grid"`
	Lyrics string `json:"lyrics"`
}

// handleImportUGParse is the wizard's "fetch and parse" step: parses an
// Ultimate Guitar tab into its semantic section list (see
// importer.ParseUGContent) instead of handing back one raw blob of text
// — the preview step that follows renders this directly, and the
// "import" step (handleImportUGConvert) re-parses the SAME url rather
// than trusting a client-echoed section list, so the conversion is
// always driven by what was actually fetched.
func (s *Server) handleImportUGParse(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		httpError(w, http.StatusBadRequest, "missing url")
		return
	}
	song, err := importer.FetchAndParseUGTab(req.URL)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	sections := make([]ugSectionPreview, len(song.Sections))
	for i, sec := range song.Sections {
		sections[i] = ugSectionPreview{Label: sec.Label, Grid: sec.Grid(), Lyrics: sec.Lyrics()}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"title": song.Title, "artist": song.Artist, "sections": sections,
	})
}

// handleImportUGConvert is the wizard's "import" step: re-fetches and
// re-parses url (never trusting a client-supplied section list — see
// handleImportUGParse's doc comment), then converts exactly the
// requested (0-based) section indices to this app's own .song-format
// text via UGSong.ToSongText.
func (s *Server) handleImportUGConvert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string `json:"url"`
		Selected []int  `json:"selected"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		httpError(w, http.StatusBadRequest, "missing url")
		return
	}
	song, err := importer.FetchAndParseUGTab(req.URL)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": song.ToSongText(req.Selected)})
}
