package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"app/internal/i18n"
)

// handleClicktrackManifest is GET /setlists/{id}/clicktrack/manifest.json:
// the click track page's own web app manifest (setlist_clicktrack.html).
// Added to a phone's Home Screen, or installed, the page then opens as an
// app of its own, straight on this set list's click track and full screen:
// on an iPhone the only full screen a page gets (fullscreen.js), and on
// Android no browser bar either. Whoever may see the set list may read it
// (its title is in it); the page asks for it with its cookies
// (crossorigin="use-credentials").
func (s *Server) handleClicktrackManifest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	person := personFromContext(r)
	canView, err := s.store.CanViewSetlistByID(r.Context(), person.ID, id)
	if err != nil || !canView {
		http.NotFound(w, r)
		return
	}
	_, meta, _, err := s.store.LoadSetlistByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	lang := s.langOf(w)
	title := orDefault(meta.Title, i18n.T(lang, "set list"))
	clickTrack := i18n.T(lang, "Click track")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "application/manifest+json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		// Its own app, apart from Songbooks itself: one per set list.
		"id":               fmt.Sprintf("/setlists/%d/clicktrack", id),
		"name":             title + " — " + clickTrack,
		"short_name":       clickTrack,
		"start_url":        setlistPath(meta) + "/clicktrack",
		"scope":            "/",
		"display":          "fullscreen",
		"display_override": []string{"fullscreen", "standalone"},
		"background_color": "#241e18",
		"theme_color":      "#241e18",
		"icons": []map[string]string{
			{"src": "/static/icon.svg", "sizes": "any", "type": "image/svg+xml", "purpose": "any"},
			{"src": "/apple-touch-icon.png", "sizes": "512x512", "type": "image/png", "purpose": "any"},
		},
	})
}
