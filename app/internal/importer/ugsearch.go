package importer

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// UGSearchResult is one Ultimate Guitar search hit worth considering — a
// public, scrapeable chord/tab page (never a "Pro" guitar-pro file,
// which has no plain-text content to parse at all — see searchTypeRank).
type UGSearchResult struct {
	SongName   string
	ArtistName string
	Type       string // "Chords" or "Tabs" — see searchTypeRank
	Rating     float64
	Votes      int
	URL        string
}

// searchTypeRank orders a result's Type by how usable it is for this
// app's purposes: a "Chords" page is exactly a simplified chord-over-
// lyric sheet (what ParseUGContent targets); a "Tabs" page is a full
// guitar tab (fret-number tablature mixed in with, or instead of, chord
// names) — still parseable as a fallback, but not the first choice.
// "Pro" (paywalled Guitar Pro files) and anything else are excluded
// entirely by SearchUG, never ranked at all.
func searchTypeRank(t string) int {
	switch t {
	case "Chords":
		return 0
	case "Tabs":
		return 1
	default:
		return 2
	}
}

// ugSearchResultsRE reuses the exact same embedded-page-data shape
// ugStoreRE/fetchUGPage already rely on — UG's search page is the same
// React app, hydrated the same way (confirmed directly against a real
// fetched search page, not assumed: it carries the identical
// class="js-store" data-content blob).
func parseUGSearchResults(body string) ([]UGSearchResult, error) {
	m := ugStoreRE.FindStringSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("couldn't find search results on this page — Ultimate Guitar may have changed its layout")
	}
	raw := html.UnescapeString(m[1])
	var store struct {
		Store struct {
			Page struct {
				Data struct {
					Results []struct {
						SongName   string  `json:"song_name"`
						ArtistName string  `json:"artist_name"`
						Type       string  `json:"type"`
						Rating     float64 `json:"rating"`
						Votes      int     `json:"votes"`
						TabURL     string  `json:"tab_url"`
					} `json:"results"`
				} `json:"data"`
			} `json:"page"`
		} `json:"store"`
	}
	if err := json.Unmarshal([]byte(raw), &store); err != nil {
		return nil, fmt.Errorf("couldn't parse search results: %w", err)
	}
	var out []UGSearchResult
	for _, r := range store.Store.Page.Data.Results {
		if r.TabURL == "" || (r.Type != "Chords" && r.Type != "Tabs") {
			continue // excludes Pro/guitar-pro (no plain-text content) and anything with no direct page link
		}
		out = append(out, UGSearchResult{
			SongName: r.SongName, ArtistName: r.ArtistName,
			Type: r.Type, Rating: r.Rating, Votes: r.Votes, URL: r.TabURL,
		})
	}
	return out, nil
}

// SearchUG searches Ultimate Guitar for title+artist and returns every
// usable (public, Chords-or-Tabs) result, ranked best first: a "Chords"
// page always sorts ahead of a "Tabs" page (see searchTypeRank), and
// within the same type, by a popularity-weighted rating
// (rating × log10(votes+1)) rather than raw rating alone — a 5.0 from 3
// voters is not more trustworthy than a 4.8 from 40,000, confirmed
// against real UG data (Hotel California's own two top "Chords"
// versions: 4.86/40755 votes vs 4.63/136 votes — the community's
// actual favorite has to win that comparison, not the other way round).
func SearchUG(title, artist string) ([]UGSearchResult, error) {
	q := strings.TrimSpace(artist + " " + title)
	if q == "" {
		return nil, fmt.Errorf("title is required")
	}
	searchURL := "https://www.ultimate-guitar.com/search.php?search_type=title&value=" + url.QueryEscape(q)
	body, err := fetch(searchURL)
	if err != nil {
		return nil, err
	}
	results, err := parseUGSearchResults(body)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(results, func(i, j int) bool {
		ri, rj := searchTypeRank(results[i].Type), searchTypeRank(results[j].Type)
		if ri != rj {
			return ri < rj
		}
		return popularityScore(results[i]) > popularityScore(results[j])
	})
	return results, nil
}

func popularityScore(r UGSearchResult) float64 {
	return r.Rating * math.Log10(float64(r.Votes)+1)
}

// wordRE splits normalized text into words for lyricsSimilarity.
var wordRE = regexp.MustCompile(`[a-z0-9']+`)

// normalizeLyricsForCompare strips this app's/UG's own chord markup
// ("[G]", "[ch]G[/ch]") and section labels, lowercases, and collapses to
// a word stream — the shared normalization both FetchAndParseUGTab's
// result (via UGSong's own Lyrics()) and a reference source's plain text
// need before a meaningful overlap comparison.
func normalizeLyricsForCompare(s string) []string {
	s = chTagRE.ReplaceAllString(s, "")
	s = regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(s, " ")
	s = strings.ToLower(s)
	return wordRE.FindAllString(s, -1)
}

// lyricsSimilarity scores how much two lyric texts overlap, 0..1, as a
// Jaccard index over normalized word SETS (not sequences — verse/chorus
// repeat structure and line-wrap differ enough between sources that a
// sequence-alignment score would under-count a genuine match; a shared-
// vocabulary measure is the robust, simple choice here, not a stand-in
// for real text diffing). Deliberately coarse: this is a sanity check
// ("is this plausibly the same song's lyrics"), not a precision edit
// distance.
func lyricsSimilarity(a, b string) float64 {
	wa, wb := normalizeLyricsForCompare(a), normalizeLyricsForCompare(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	setA := make(map[string]bool, len(wa))
	for _, w := range wa {
		setA[w] = true
	}
	setB := make(map[string]bool, len(wb))
	for _, w := range wb {
		setB[w] = true
	}
	inter := 0
	for w := range setA {
		if setB[w] {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// allLyrics concatenates every section's lyrics, for a whole-song
// similarity comparison.
func (s *UGSong) allLyrics() string {
	var b strings.Builder
	for _, sec := range s.Sections {
		b.WriteString(sec.Lyrics())
		b.WriteByte('\n')
	}
	return b.String()
}

// minLyricsSimilarity is the threshold a candidate's lyrics must clear
// against a reference source to be accepted as "verified" rather than
// just "best-ranked, unverified" — picked well below 1.0 on purpose:
// different transcriptions legitimately vary (repeated choruses
// written out once vs "x2", spelled-out ad-libs, bracketed backing
// vocals), so demanding a near-exact match would reject good matches as
// often as it catches bad ones. 0.5 still firmly rejects a wrong song
// (two different songs' lyrics essentially never share half their
// vocabulary) while tolerating normal transcription variance.
const minLyricsSimilarity = 0.5

// maxUGCandidates bounds how many search results FindBestUGTab actually
// fetches and parses — each one is a real network request, and the
// ranked list already puts the most likely candidates first.
const maxUGCandidates = 4

// UGFindResult is FindBestUGTab's full outcome — the chosen tab already
// parsed, plus enough about HOW it was chosen for the caller to show the
// user (never silently auto-apply without saying why).
type UGFindResult struct {
	Song       *UGSong
	URL        string
	Candidates int     // how many UG results were actually fetched and checked
	Verified   bool    // true if Similarity cleared minLyricsSimilarity against a reference
	Similarity float64 // 0 if no reference lyrics were available to compare against at all
}

// FindBestUGTab is the "search mode" the manual search-link-and-paste
// flow used to require by hand: searches Ultimate Guitar, fetches and
// parses the top-ranked public Chords/Tabs candidates (see SearchUG),
// and — when a reference lyrics source is available — picks the first
// one whose lyrics plausibly match it (see lyricsSimilarity), checked in
// ranked order so a popular, well-rated version still wins among
// several that all pass the lyrics check. A reference comes from Genius
// when SONGBOOKS_GENIUS_TOKEN is configured (see SearchGenius); with no
// token, or no Genius hit, this falls back to the single best-ranked
// candidate, honestly marked Verified:false rather than silently
// claiming a check that never happened.
func FindBestUGTab(title, artist string) (*UGFindResult, error) {
	results, err := SearchUG(title, artist)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no Ultimate Guitar chord/tab results for %q", strings.TrimSpace(artist+" "+title))
	}

	var refLyrics string
	if geniusURL, err := SearchGenius(title, artist); err == nil {
		if text, _, err := FetchLyrics(geniusURL); err == nil {
			refLyrics = text
		}
	}

	n := len(results)
	if n > maxUGCandidates {
		n = maxUGCandidates
	}
	var fallback *UGFindResult
	for i := 0; i < n; i++ {
		song, err := FetchAndParseUGTab(results[i].URL)
		if err != nil {
			continue // a dead/changed link among several candidates — try the next, don't fail the whole search
		}
		res := &UGFindResult{Song: song, URL: results[i].URL, Candidates: i + 1}
		if fallback == nil {
			fallback = res // first one that at least PARSED, used if nothing verifies
		}
		if refLyrics == "" {
			continue // nothing to verify against — keep checking, but can't mark any of them Verified
		}
		sim := lyricsSimilarity(song.allLyrics(), refLyrics)
		if sim >= minLyricsSimilarity {
			res.Verified, res.Similarity = true, sim
			return res, nil
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("found %d Ultimate Guitar result(s) but couldn't fetch/parse any of them", n)
}
