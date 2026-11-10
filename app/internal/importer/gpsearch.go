package importer

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// GPSearchResult is one GProTab.net search hit — a tab page, not yet a
// fetched file. GProTab.net (unlike Ultimate Guitar — see ugsearch.go's
// own doc comment on why that site is excluded here) serves every
// result as a plain, anonymous, directly downloadable Guitar Pro binary
// with no login and no robots.txt restriction on either /en/search or
// /en/tabs/ (confirmed directly against the live site and its
// robots.txt before writing this), which is the whole reason this
// imports from here instead.
type GPSearchResult struct {
	SongName   string
	ArtistName string
	URL        string // tab page URL, e.g. https://gprotab.net/en/tabs/the-eagles/hotel-california
}

// gpSearchTabRE matches one "tab search-tab" result block on a GProTab.net
// search results page — confirmed directly against the live, server-
// rendered HTML (no JS rendering involved, no embedded JSON blob the way
// Ultimate Guitar's React hydration works — see ugsearch.go): each result
// is a plain <div class="tab search-tab"> holding exactly one artist link
// (class="tab-band") and one tab link (class="tab-name").
var gpSearchTabRE = regexp.MustCompile(`(?s)<div class="tab search-tab">.*?<a href="(/en/tabs/[^"]+)" class="tab-band">([^<]*)</a>.*?<a href="(/en/tabs/[^"]+)" class="tab-name">([^<]*)</a>`)

// SearchGP searches GProTab.net for title (artist is used only to RANK
// the results afterward, via matchesGPQuery/gpQueryScore against each
// result's own artist field — never folded into the query string
// itself) and returns every result, ranked by how well it matches —
// best first. Unlike SearchUG, this has no "Pro vs public" distinction
// to make (every GProTab.net result already IS a Guitar Pro file) and no
// rating/popularity data on the search page itself (that only appears on
// the individual tab page — see FetchGPMeta), so ranking here is purely
// query-match quality; the caller decides how many candidates are worth
// fetching metadata for.
//
// title-only, deliberately: confirmed live, GProTab.net's own search
// silently returns ZERO results for several real multi-word queries that
// combine a title with an artist name ("Eagles Hotel California",
// "Sultans of Swing Dire Straits" — both empty), while the exact same
// title alone, or title-then-single-word-artist, reliably returns
// results ("Hotel California", "Hotel California Eagles", "Shape Of My
// Heart Sting" — all non-empty). Whatever their search is actually doing
// with multiple query terms, it isn't simple multi-word OR/AND matching,
// and it's NOT robust to an artist name being included — so artist is
// kept out of the request entirely and used purely to rank/filter
// results this app already has in hand.
func SearchGP(title, artist string) ([]GPSearchResult, error) {
	q := strings.TrimSpace(title)
	if q == "" {
		return nil, fmt.Errorf("title is required")
	}
	searchURL := "https://www.gprotab.net/en/search?q=" + url.QueryEscape(q)
	body, err := fetch(searchURL)
	if err != nil {
		return nil, err
	}
	var results []GPSearchResult
	for _, m := range gpSearchTabRE.FindAllStringSubmatch(body, -1) {
		results = append(results, GPSearchResult{
			ArtistName: unescapeEntities(strings.TrimSpace(m[2])),
			SongName:   unescapeEntities(strings.TrimSpace(m[4])),
			URL:        "https://gprotab.net" + m[3],
		})
	}
	filtered := results[:0]
	for _, r := range results {
		if matchesGPQuery(title, artist, r) {
			filtered = append(filtered, r)
		}
	}
	results = filtered
	sort.SliceStable(results, func(i, j int) bool {
		return gpQueryScore(title, artist, results[i]) > gpQueryScore(title, artist, results[j])
	})
	return results, nil
}

// matchesGPQuery/gpQueryScore reuse the exact same Jaccard word-overlap
// approach matchesQuery (ugsearch.go) already uses for Ultimate Guitar —
// same reasoning applies: a site's own search is loose/fuzzy, so results
// that don't actually resemble what was asked for need filtering before
// ranking, not after.
func matchesGPQuery(queryTitle, queryArtist string, r GPSearchResult) bool {
	if jaccard(normalizeWords(queryTitle), normalizeWords(r.SongName)) < minTitleMatch {
		return false
	}
	if strings.TrimSpace(queryArtist) != "" &&
		jaccard(normalizeWords(queryArtist), normalizeWords(r.ArtistName)) < minArtistMatch {
		return false
	}
	return true
}

func gpQueryScore(queryTitle, queryArtist string, r GPSearchResult) float64 {
	score := jaccard(normalizeWords(queryTitle), normalizeWords(r.SongName))
	if strings.TrimSpace(queryArtist) != "" {
		score += jaccard(normalizeWords(queryArtist), normalizeWords(r.ArtistName))
	}
	return score
}

// GPTabMeta is what a GProTab.net tab page itself carries about the file
// — shown to the reader so they can pick a candidate without downloading
// every one first (confirmed live: this is exactly what the real tab
// page's own "info" table shows — format, approximate size, rating,
// download count).
type GPTabMeta struct {
	Format    string  // "gp3", "gp4", "gp5", "gpx"
	SizeKB    float64 // 0 if not found on the page
	Rating    float64 // 0..5, 0 if no votes yet
	Votes     int
	Downloads int
}

var (
	gpFormatRE    = regexp.MustCompile(`(?s)<td>Tab file type</td><td>([a-z0-9]+)</td>`)
	gpSizeRE      = regexp.MustCompile(`(?s)<td>File size</td><td>~?([0-9.]+)\s*kb</td>`)
	gpDownloadsRE = regexp.MustCompile(`(?s)<td>Times downloaded</td><td>([0-9]+)</td>`)
	gpRatingRE    = regexp.MustCompile(`(?s)<strong>([0-9.]+)/5</strong>\s*-\s*<u>([0-9]+)\s*votes?</u>`)
)

// FetchGPMeta fetches a GProTab.net tab page (not the file itself — see
// FetchGPFile) and extracts its own info-table fields by plain regex,
// the same "no DOM parser, just targeted patterns against known markup"
// approach stripTags/parseUGSearchResults already use elsewhere in this
// package. Every field is best-effort: a missing/changed field leaves
// its zero value rather than failing the whole fetch, since none of
// them are required to actually import the file.
func FetchGPMeta(tabURL string) (*GPTabMeta, error) {
	body, err := fetch(tabURL)
	if err != nil {
		return nil, err
	}
	meta := &GPTabMeta{}
	if m := gpFormatRE.FindStringSubmatch(body); m != nil {
		meta.Format = m[1]
	}
	if m := gpSizeRE.FindStringSubmatch(body); m != nil {
		meta.SizeKB, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := gpDownloadsRE.FindStringSubmatch(body); m != nil {
		meta.Downloads, _ = strconv.Atoi(m[1])
	}
	if m := gpRatingRE.FindStringSubmatch(body); m != nil {
		meta.Rating, _ = strconv.ParseFloat(m[1], 64)
		meta.Votes, _ = strconv.Atoi(m[2])
	}
	return meta, nil
}

// maxGPCandidates bounds how many search results get their metadata
// fetched (one extra request each) — same reasoning as maxUGCandidates.
const maxGPCandidates = 8

// GPCandidate is one fully-described search result, ready to show the
// reader a real choice (not just a bare link) — SearchGP's own result
// plus whatever FetchGPMeta found for it. MetaErr is set (and the rest
// left zero) rather than dropping the candidate entirely when metadata
// fetch fails — a reader can still pick it and let the actual file fetch
// (FetchGPFile) be the real test.
type GPCandidate struct {
	GPSearchResult
	GPTabMeta
	MetaErr string `json:"MetaErr,omitempty"`
}

// FindGPCandidates searches GProTab.net and fetches metadata for the top
// maxGPCandidates matches, so the caller can show a real pick-one list
// (title, artist, format, size, rating) instead of bare links — this app
// always shows the reader a choice here rather than silently committing
// to one guess, unlike FindBestUGTab's auto-pick (that flow has a plain-
// text lyrics cross-check to verify against; a binary GP file has
// nothing equivalent to verify with, so a human pick is the right
// substitute, not a weaker auto-pick).
func FindGPCandidates(title, artist string) ([]GPCandidate, error) {
	results, err := SearchGP(title, artist)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no GProTab.net results for %q", strings.TrimSpace(artist+" "+title))
	}
	n := len(results)
	if n > maxGPCandidates {
		n = maxGPCandidates
	}
	out := make([]GPCandidate, n)
	for i := 0; i < n; i++ {
		out[i].GPSearchResult = results[i]
		if meta, err := FetchGPMeta(results[i].URL); err != nil {
			out[i].MetaErr = err.Error()
		} else {
			out[i].GPTabMeta = *meta
		}
	}
	return out, nil
}

// maxGPFileSize caps how large a "Guitar Pro file" download this app
// will accept — a real one is a few hundred KB at most (plain note/
// tuning/timing data, no audio); anything bigger than this is either a
// mistake or something this app has no business fetching automatically.
const maxGPFileSize = 8 << 20 // 8MB

// FetchGPFile downloads the actual Guitar Pro binary for a GProTab.net
// tab page — confirmed live: appending "?download" to the tab page's own
// URL (the exact link its own "Download" button points to) returns the
// raw file directly, anonymously, Content-Type: application/octet-
// stream, Content-Disposition: attachment — no login, no redirect
// chain, no JS-triggered request to reverse-engineer. Returns the raw
// bytes and the filename the server itself suggests (Content-
// Disposition), for the caller to hand straight to alphaTab's own
// ScoreLoader.loadScoreFromBytes client-side, same as a manually
// dropped file.
func FetchGPFile(tabURL string) (data []byte, filename string, err error) {
	req, err := http.NewRequest(http.MethodGet, tabURL+"?download", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch: %s returned %s", tabURL, resp.Status)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxGPFileSize))
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("empty response — this tab may no longer have a downloadable file")
	}
	filename = "import.gp"
	if _, params, cdErr := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); cdErr == nil && params["filename"] != "" {
		filename = params["filename"]
	}
	return data, filename, nil
}
