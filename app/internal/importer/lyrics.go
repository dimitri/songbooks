package importer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var geniusMarkerRE = regexp.MustCompile(`<div[^>]*data-lyrics-container="true"[^>]*>`)
var divOpenCloseRE = regexp.MustCompile(`(?i)<div\b|</div>`)

// extractBalancedDivs finds every div opened by marker in body and returns
// each one's inner HTML, tracking nested <div>/</div> depth to find the
// correct matching close tag — regexp alone can't do this (HTML nesting
// isn't a regular language), so this walks tag-by-tag instead.
func extractBalancedDivs(body string, marker *regexp.Regexp) []string {
	var out []string
	pos := 0
	for {
		loc := marker.FindStringIndex(body[pos:])
		if loc == nil {
			break
		}
		start := pos + loc[1] // just after the opening <div ...> tag
		depth := 1
		i := start
		for depth > 0 {
			m := divOpenCloseRE.FindStringIndex(body[i:])
			if m == nil {
				i = len(body)
				break
			}
			tag := body[i+m[0] : i+m[1]]
			if strings.EqualFold(tag, "</div>") {
				depth--
			} else {
				depth++
			}
			i += m[1]
		}
		end := i
		if strings.HasSuffix(body[:end], "</div>") {
			end -= len("</div>")
		}
		out = append(out, body[start:end])
		pos = i
	}
	return out
}

// FetchLyrics fetches url and returns a best-effort plain-text draft.
// Genius pages get a targeted extraction (its lyrics containers are
// reliably marked); anything else falls back to whole-page text, which
// includes surrounding site chrome the user is expected to trim — this is
// a convenience starting point, not a guaranteed clean scrape.
func FetchLyrics(url string) (text string, source string, err error) {
	body, err := fetch(url)
	if err != nil {
		return "", "", err
	}
	if strings.Contains(url, "genius.com") {
		parts := extractBalancedDivs(body, geniusMarkerRE)
		// Genius's page carries more than one element with this same
		// marker (the real lyrics plus, at least, an unrelated share/embed
		// widget) — the real one is reliably the longest block that actually
		// contains Genius's own "[Section]" markers; a short or
		// bracket-less match is chrome, not lyrics.
		best := ""
		for _, p := range parts {
			t := stripTags(p)
			if strings.Contains(t, "[") && len(t) > len(best) {
				best = t
			}
		}
		if best != "" {
			// Genius prefixes the real lyrics with a contributor/translation
			// nav and a "Read More" song-description annotation inside the
			// same wrapper; real lyrics always open on a "[Section]" tag, so
			// anything before the first one is that chrome, not lyrics.
			if i := strings.Index(best, "["); i > 0 {
				best = best[i:]
			}
			return best, "genius", nil
		}
		return "", "", fmt.Errorf("couldn't find a lyrics container on this Genius page — paste the text instead")
	}
	return stripTags(body), "generic", nil
}

// ErrGeniusNotConfigured is returned by SearchGenius when no API token is
// configured on this deployment — a distinct sentinel so the HTTP handler
// (and the editor UI) can show "automatic search isn't configured, paste a
// URL instead" rather than a raw network/API error.
var ErrGeniusNotConfigured = fmt.Errorf("Genius API search isn't configured on this deployment (set SONGBOOKS_GENIUS_TOKEN) — paste a URL instead")

// SearchGenius resolves a title+artist to a real Genius song page URL via
// Genius's own documented search API (api.genius.com/search) — a sanctioned
// data endpoint, not a scrape of Genius's search-results HTML page. Unlike
// FetchLyrics (any URL the user already has), this one API call REQUIRES a
// client access token — confirmed directly against the live API (a keyless
// request returns 401 "This call requires an access_token"), not assumed.
// No token is configured on this deployment by default (see
// ErrGeniusNotConfigured); an operator who wants one-click search needs to
// create a free client at genius.com/developers and set
// SONGBOOKS_GENIUS_TOKEN.
func SearchGenius(title, artist string) (songURL string, err error) {
	token := os.Getenv("SONGBOOKS_GENIUS_TOKEN")
	if token == "" {
		return "", ErrGeniusNotConfigured
	}
	q := strings.TrimSpace(artist + " " + title)
	req, err := http.NewRequest(http.MethodGet, "https://api.genius.com/search?q="+url.QueryEscape(q), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("genius search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("genius search: returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var parsed struct {
		Response struct {
			Hits []struct {
				Result struct {
					URL string `json:"url"`
				} `json:"result"`
			} `json:"hits"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("genius search: unexpected response: %w", err)
	}
	if len(parsed.Response.Hits) == 0 {
		return "", fmt.Errorf("no Genius results for %q — try a manual search instead", q)
	}
	return parsed.Response.Hits[0].Result.URL, nil
}

// GoogleLyricsSearchURL builds a plain Google search URL for the song, to
// open in a new tab — this app never scrapes Google search results
// (against its ToS and blocked in practice); the user finds a source page
// there and pastes its URL into FetchLyrics instead.
func GoogleLyricsSearchURL(artist, title string) string {
	q := strings.TrimSpace(artist + " " + title + " lyrics")
	return "https://www.google.com/search?q=" + url.QueryEscape(q)
}
