package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"app/internal/importer"
	"app/internal/irealpro"
	"app/internal/tab"
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

// handleImportChords is the song editor's single-step "Fetch" Import tab
// (see play.html's import-wizard): unlike the full multi-step
// pick-sections wizard (handleImportUGParse/handleImportUGConvert, used
// when creating a brand-new song), this has no section-picker UI, so it
// converts every parsed section via UGSong.ToSongText straight away —
// the result already uses this app's own inline "[Chord]word" syntax
// (not UG's raw chord-line-above-lyric-line layout), landed in the same
// review textarea the lyrics path already uses for the user to edit
// before applying.
func (s *Server) handleImportChords(w http.ResponseWriter, r *http.Request) {
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
	all := make([]int, len(song.Sections))
	for i := range all {
		all[i] = i
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"text": song.ToSongText(all), "title": song.Title, "artist": song.Artist, "capo": song.Capo,
	})
}

// handleImportUGFind is the import wizard's one-click "find & fetch"
// step for the common case: searches Ultimate Guitar for title+artist,
// ranks the public chord/tab results, fetches the best candidates in
// order, and — when Genius search is configured (SONGBOOKS_GENIUS_
// TOKEN) — cross-checks each one's lyrics against a Genius reference
// before accepting it (see importer.FindBestUGTab). Returns the same
// {"text": ...} shape handleImportChords already does, plus url/
// confidence/verified so the review step can show HOW the draft was
// chosen rather than silently presenting it as certain.
func (s *Server) handleImportUGFind(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	artist := r.URL.Query().Get("artist")
	if title == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	res, err := importer.FindBestUGTab(title, artist)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	all := make([]int, len(res.Song.Sections))
	for i := range all {
		all[i] = i
	}
	confidence := "picked the highest-rated public Ultimate Guitar version — lyrics couldn't be cross-checked, please review"
	if res.Verified {
		confidence = fmt.Sprintf("lyrics cross-checked against Genius (%.0f%% match)", res.Similarity*100)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"text": res.Song.ToSongText(all), "url": res.URL, "confidence": confidence, "verified": res.Verified,
		"title": res.Song.Title, "artist": res.Song.Artist, "capo": res.Song.Capo,
	})
}

// iRealTuneResult is what the import wizard's iReal step needs about a
// found tune — enough to show "here's what we found" and drive the
// merge-preview/apply calls, without exposing irealpro.Tune itself.
type iRealTuneResult struct {
	Title, Key, Style string
	SourceURL         string
	Cached            bool
}

// findIRealTune is the find step's shared core: check the pre-built
// cache first (instant — see store.FindIRealSong), and only if that
// misses, fall back to a live, best-effort scan of the forum's own
// category pages (irealpro.FindOnForum) — slower (a handful of
// requests) and not guaranteed to find anything, since there's no real
// search endpoint to use (see that function's doc comment), but a live
// hit is cached immediately so the same song is instant next time.
// Returns the chart's own grid segments directly (not a *Tune) — a
// cache hit only ever has the already-flattened, already-simplified
// grid text (store.IRealSong.GridText), not a re-parseable Tune with
// real section letters, so segments (what MergePreviewSegments
// actually needs) is the right shared shape for both paths.
func (s *Server) findIRealTune(ctx context.Context, title, artist string) (segments []string, res iRealTuneResult, err error) {
	if cached, cerr := s.store.FindIRealSong(ctx, title); cerr == nil {
		return []string{cached.GridText}, iRealTuneResult{
			Title: cached.Title, Key: cached.Key, Style: cached.Style,
			SourceURL: cached.SourceURL, Cached: true,
		}, nil
	}
	tunes, threadURL, ferr := irealpro.FindOnForum(title, artist)
	if ferr != nil {
		return nil, iRealTuneResult{}, ferr
	}
	// Pick whichever parsed tune's own title best matches what was
	// asked for (a thread can carry several songs' worth of links).
	var best *irealpro.Tune
	bestScore := -1.0
	wantWords := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(title)) {
		wantWords[w] = true
	}
	for _, t := range tunes {
		score := 0.0
		for _, w := range strings.Fields(strings.ToLower(t.Title)) {
			if wantWords[w] {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = t, score
		}
	}
	return best.Segments(), iRealTuneResult{
		Title: best.Title, Key: best.Key, Style: best.Style,
		SourceURL: threadURL, Cached: false,
	}, nil
}

// handleImportIRealFind is the import wizard's iReal step: looks up a
// bar-accurate chord reference for title+artist (cache, then a best-
// effort live forum scan — see findIRealTune), and if found, caches it
// for next time. Returns just the tune's identity (not yet a merge —
// the client calls handleImportIRealMerge separately, passing the
// current draft text, once it has this result).
func (s *Server) handleImportIRealFind(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	artist := r.URL.Query().Get("artist")
	if title == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	_, res, err := s.findIRealTune(r.Context(), title, artist)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleImportIRealMerge previews (POST with apply=false, the default)
// or applies (apply=true, with acceptLabels naming which sections) the
// section-aware iReal/UG merge against currentText — see
// irealpro.MergePreview/ApplyMerge's own doc comments for the matching
// and transposition-safety rules. Never writes to the database; the
// caller (the Review step) puts the returned text into its own
// textarea for the user to look at and edit like any other draft.
func (s *Server) handleImportIRealMerge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentText string   `json:"currentText"`
		Title       string   `json:"title"`
		Artist      string   `json:"artist"`
		Apply       bool     `json:"apply"`
		AcceptLabels  []string `json:"acceptLabels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CurrentText == "" || req.Title == "" {
		httpError(w, http.StatusBadRequest, "currentText and title are required")
		return
	}
	segments, _, err := s.findIRealTune(r.Context(), req.Title, req.Artist)
	if err != nil || len(segments) == 0 {
		httpError(w, http.StatusNotFound, "no iReal chart found for this song")
		return
	}
	candidates := irealpro.MergePreviewSegments(req.CurrentText, segments)
	if !req.Apply {
		writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
		return
	}
	accept := map[string]bool{}
	for _, l := range req.AcceptLabels {
		accept[l] = true
	}
	merged := irealpro.ApplyMerge(req.CurrentText, candidates, accept)
	writeJSON(w, http.StatusOK, map[string]any{"text": merged})
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

// gpCandidateJSON is one GProTab.net search result, shaped for the GP
// import wizard's own search step (gp-import.js) — a reader picks one
// of these, not an auto-chosen best guess (see importer.FindGPCandidates'
// own doc comment for why: unlike the UG find flow, there's no plain-
// text lyrics cross-check to verify a binary GP file against, so a
// human pick is the substitute, not a weaker auto-pick).
type gpCandidateJSON struct {
	Title, Artist, URL string
	Format             string
	SizeKB             float64
	Rating             float64
	Votes, Downloads   int
	MetaErr            string `json:"MetaErr,omitempty"`
}

// handleImportGPFind is GET /api/import/gp/find?title=...&artist=... —
// the GP import wizard's own search step, searching GProTab.net (see
// importer.FindGPCandidates' own doc comment for why that site, not
// Ultimate Guitar: UG's robots.txt explicitly disallows /tab-pro/download
// and /pro/, exactly the paths a Guitar Pro file download would need;
// GProTab.net has no such restriction and serves every result as a
// plain, anonymous, directly downloadable file).
func (s *Server) handleImportGPFind(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	artist := r.URL.Query().Get("artist")
	if title == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	candidates, err := importer.FindGPCandidates(title, artist)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]gpCandidateJSON, len(candidates))
	for i, c := range candidates {
		out[i] = gpCandidateJSON{
			Title: c.SongName, Artist: c.ArtistName, URL: c.URL,
			Format: c.Format, SizeKB: c.SizeKB,
			Rating: c.Rating, Votes: c.Votes, Downloads: c.Downloads,
			MetaErr: c.MetaErr,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": out})
}

// gpFetchAllowedHostSuffix restricts handleImportGPFetch to fetching
// from the one site this app actually knows how to download a Guitar
// Pro file from — handleImportGPFind only ever returns gprotab.net URLs
// itself, but this endpoint takes a URL straight from the request query
// string, so it re-validates rather than trusting that a client only
// ever echoes back what find handed it.
const gpFetchAllowedHostSuffix = "gprotab.net"

// handleImportGPFetch is GET /api/import/gp/fetch?url=<tab-page-url> —
// downloads the actual Guitar Pro binary for a candidate the reader
// picked from handleImportGPFind's own results, and streams it straight
// back with the same Content-Type/Content-Disposition the source site
// itself sent, so the client can read it as raw bytes and hand them to
// alphaTab's own ScoreLoader.loadScoreFromBytes — exactly the same path
// a manually-dropped file already takes (gp-import.js), just sourced
// from a fetch instead of a <input type=file>.
func (s *Server) handleImportGPFetch(w http.ResponseWriter, r *http.Request) {
	tabURL := r.URL.Query().Get("url")
	if tabURL == "" {
		httpError(w, http.StatusBadRequest, "missing url")
		return
	}
	u, err := url.Parse(tabURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.HasSuffix(u.Hostname(), gpFetchAllowedHostSuffix) {
		httpError(w, http.StatusBadRequest, "url must be a "+gpFetchAllowedHostSuffix+" tab page")
		return
	}
	data, filename, err := importer.FetchGPFile(tabURL)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Write(data)
}

// handleGPImportPreview is POST /api/gp-import/preview — the GP import
// wizard's own "preview before loading" step (gp-import.js): given one
// section's already-extracted riff (the same JSON shape putScore/
// handlePutSectionScore stores, produced client-side by extractRiff),
// renders it to alphaTeX for the vendored alphaTab JS renderer to draw,
// so a reader sees real notation for exactly what they're about to
// import before committing to it — not just the bar-count mapping table
// this wizard showed before. Stateless and variant-agnostic (a pure
// riff-&gt;alphaTeX transform, same tab.ToAlphaTex call attachTabRiffs
// already makes for every other TabOnly row in this app), so this needs
// no variant id and writes nothing.
func (s *Server) handleGPImportPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Riff string `json:"riff"` // JSON-encoded Riff, as a STRING — same double-encoding putScore (gp-import.js) already sends, not a nested JSON object, so this previews the exact bytes that would be written
		// The song header the import is about to write (gp-import.js's
		// header choices): the preview is drawn in that time signature
		// and swing, as the Song view will draw it.
		Time  string `json:"time"`
		Swing string `json:"swing"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	riff, err := tab.DecodeRiffJSON([]byte(req.Riff))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid riff: "+err.Error())
		return
	}
	clef := ""
	if len(riff.Tuning) <= 4 {
		clef = "F4"
	}
	tex := tab.ToAlphaTex(riff, clef, nil)
	num, den := timeSigParts(orDefault(req.Time, "4/4"))
	if meta := tab.FirstBarMeta(atoiOr(num, 4), atoiOr(den, 4), req.Swing); meta != "" {
		tex = strings.Replace(tex, ".\n", ".\n"+meta, 1)
	}
	writeJSON(w, http.StatusOK, map[string]string{"alphaTex": tex})
}
