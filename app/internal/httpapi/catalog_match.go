package httpapi

import (
	"net/http"
	"strings"
)

// libraryMatchHit is one row of handleCatalogMatch: enough for a new-song
// form to say "already in the library" and tell versions apart.
type libraryMatchHit struct {
	VariantID  int64  `json:"variantId"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Key        string `json:"key"`
	Visibility string `json:"visibility"`
	MadeBy     string `json:"madeBy"`
	Bands      int    `json:"bands"`
	Songbooks  int    `json:"songbooks"`
	Exact      bool   `json:"exact"`
}

const maxLibraryMatches = 5

// handleCatalogMatch is GET /api/catalog/match?title=...&artist=... —
// the "is this song already in the library?" check every new-song form
// runs as a title is typed (static/js/library-match.js). A separate
// endpoint from /api/catalog/search because that one is English full-text
// search over title, artist AND lyrics: it misses accents and leading
// articles and matches songs that merely quote the words — see
// store.MatchLibrary for the title/artist matching used here instead.
func (s *Server) handleCatalogMatch(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	title := clipRunes(strings.TrimSpace(r.URL.Query().Get("title")), 200)
	artist := clipRunes(strings.TrimSpace(r.URL.Query().Get("artist")), 200)
	if title == "" {
		writeJSON(w, http.StatusOK, []libraryMatchHit{})
		return
	}
	matches, err := s.store.MatchLibrary(r.Context(), person.ID, title, artist, maxLibraryMatches)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hits := make([]libraryMatchHit, len(matches))
	for i, m := range matches {
		hits[i] = libraryMatchHit{
			VariantID: m.VariantID, Title: m.Title, Artist: m.Artist, Key: m.Key,
			Visibility: m.Visibility, MadeBy: m.MadeBy, Bands: m.Bands, Songbooks: m.Songbooks, Exact: m.Exact,
		}
	}
	writeJSON(w, http.StatusOK, hits)
}

// clipRunes bounds what a type-ahead query can make the database chew on.
func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
