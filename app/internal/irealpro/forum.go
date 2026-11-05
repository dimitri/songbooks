package irealpro

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// This file is the live, on-demand counterpart to the offline harvester
// (see cmd/irealcrawl, not part of this package): given a title/artist
// the pre-built cache doesn't have, do a best-effort scan of the
// forum's own category index pages for a matching thread, then fetch
// and parse it directly. There's no general search here — the forum's
// own search feature is disallowed in robots.txt — this only ever
// follows ordinary, allowed category/thread pages, the same ones the
// offline harvest already crawled.

// forumCategories mirrors cmd/irealcrawl's own list — kept here too
// since a live, interactive lookup only has time to check the first
// page of each (a handful of requests, a few seconds), not the
// hundreds of pages deep a category can run; this is explicitly best-
// effort, not exhaustive.
var forumCategories = []string{
	"pop-rock-blues-indie.7",
	"jazz.6",
	"brazilian-latin.17",
	"country-folk-bluegrass.12",
	"holiday-film-worship-classical-world-other.13",
}

var (
	forumThreadLinkRE = regexp.MustCompile(`href="(/threads/[a-z0-9-]+\.[0-9]+/)"[^>]*class="[^"]*"[^>]*>([^<]+)<`)
	forumIrealLinkRE  = regexp.MustCompile(`irealb://[^"'<> ]+`)
)

func forumFetch(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; songbooks-import/1.0; on-demand lookup)")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s returned %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return string(body), err
}

// titleWords/overlapScore are a small, self-contained word-overlap
// check (not shared with internal/importer's own matchesQuery, to keep
// this package dependency-free of that one) — good enough to tell
// "this thread title is plausibly about this song" from "this is
// unrelated," the same bar the harvester's own matching already clears.
var wordSplitRE = regexp.MustCompile(`[a-z0-9']+`)

func titleWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordSplitRE.FindAllString(strings.ToLower(s), -1) {
		out[w] = true
	}
	return out
}

func overlapScore(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	return float64(inter) / float64(len(a))
}

// ForumMatch is one candidate thread FindOnForum turned up.
type ForumMatch struct {
	ThreadURL string
	Title     string
	Score     float64
}

// FindOnForum does a best-effort scan of the forum's category index
// pages (first page of each only) for a thread whose own title
// plausibly matches title+artist, then fetches and parses the best
// one. Returns the parsed tunes from that thread (there may be more
// than one song's worth of irealb:// links on a single thread, same as
// the offline harvest saw) plus the thread URL, or an error if nothing
// matched well enough.
func FindOnForum(title, artist string) (tunes []*Tune, threadURL string, err error) {
	want := titleWords(title + " " + artist)
	var best ForumMatch
	for _, cat := range forumCategories {
		body, ferr := forumFetch("https://forums.irealpro.com/forums/" + cat + "/")
		if ferr != nil {
			continue // one category failing to load shouldn't fail the whole scan
		}
		for _, m := range forumThreadLinkRE.FindAllStringSubmatch(body, -1) {
			threadTitle := html.UnescapeString(strings.TrimSpace(m[2]))
			score := overlapScore(want, titleWords(threadTitle))
			if score > best.Score {
				best = ForumMatch{ThreadURL: "https://forums.irealpro.com" + m[1], Title: threadTitle, Score: score}
			}
		}
	}
	if best.Score < 0.5 {
		return nil, "", fmt.Errorf("no plausible match found on the forum's own category pages")
	}

	body, err := forumFetch(best.ThreadURL)
	if err != nil {
		return nil, "", err
	}
	links := forumIrealLinkRE.FindAllString(body, -1)
	if len(links) == 0 {
		return nil, "", fmt.Errorf("found a matching thread %q but it carries no chord chart links", best.Title)
	}
	for _, link := range links {
		parsed, perr := ParseURL(link)
		if perr != nil {
			continue
		}
		tunes = append(tunes, parsed...)
	}
	if len(tunes) == 0 {
		return nil, "", fmt.Errorf("found a matching thread %q but couldn't parse any of its chart links", best.Title)
	}
	return tunes, best.ThreadURL, nil
}
