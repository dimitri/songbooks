package importer

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
)

// ugStoreRE finds UG's embedded page-data blob: a div carrying the whole
// page's data as an HTML-escaped JSON attribute. This is the documented
// shape of their React app's hydration data, not public API — it may break
// if UG changes their frontend, same caveat as any scraped site.
var ugStoreRE = regexp.MustCompile(`(?s)class="js-store"[^>]*data-content="([^"]*)"`)

var chTagRE = regexp.MustCompile(`\[/?ch\]`)

// FetchUGTab fetches an Ultimate Guitar tab page and returns its chord/lyric
// body as a draft in this app's own "[Chord]word" syntax — UG's tab text
// uses the same idea with "[ch]Chord[/ch]", so the translation is a
// straight tag swap. The result is tab-formatted text (chords on their own
// line above lyrics, not yet split into bars), landed in the raw editor for
// the user to fold into [Section]/grid: lines — Ultimate Guitar tabs are
// frequently wrong or just differently arranged, so this is explicitly a
// starting point to correct, never an authoritative import.
func FetchUGTab(pageURL string) (text string, err error) {
	body, err := fetch(pageURL)
	if err != nil {
		return "", err
	}
	m := ugStoreRE.FindStringSubmatch(body)
	if m == nil {
		return "", fmt.Errorf("couldn't find tab data on this page — Ultimate Guitar may have changed its layout, or this isn't a tab page")
	}
	raw := html.UnescapeString(m[1])

	var store struct {
		Store struct {
			Page struct {
				Data struct {
					TabView struct {
						WikiTab struct {
							Content string `json:"content"`
						} `json:"wiki_tab"`
					} `json:"tab_view"`
				} `json:"data"`
			} `json:"page"`
		} `json:"store"`
	}
	if err := json.Unmarshal([]byte(raw), &store); err != nil {
		return "", fmt.Errorf("couldn't parse tab data: %w", err)
	}
	content := store.Store.Page.Data.TabView.WikiTab.Content
	if content == "" {
		return "", fmt.Errorf("this page has no tab content (chord diagrams / Pro tabs aren't supported — use a plain chord/lyric tab page)")
	}

	// UG's [ch]G[/ch] -> our [G]; [tab]...[/tabyes] wrapper tags are just
	// layout markers here and are dropped.
	content = chTagRE.ReplaceAllStringFunc(content, func(m string) string {
		if m == "[ch]" {
			return "["
		}
		return "]"
	})
	content = regexp.MustCompile(`\[/?tab\]`).ReplaceAllString(content, "")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.TrimSpace(content), nil
}
