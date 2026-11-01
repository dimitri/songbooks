// Package importer fetches a single page the user gives a URL for and pulls
// out a best-effort draft (lyrics text, or an Ultimate Guitar chord/lyric
// tab) for the prep editor. Nothing here is ever saved automatically — the
// result always lands in a textarea the user reviews and fixes, since
// scraped markup varies by site and changes over time. This is a personal,
// single-fetch convenience (one URL the user already has), not a search
// crawler or a bulk-scraping tool.
package importer

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

const userAgent = "songbooks-app/0.1 (personal lyrics/chord import; +local use only)"

func fetch(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch: %s returned %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8MB cap — a lyrics/tab page is never this large
	if err != nil {
		return "", err
	}
	return string(body), nil
}

var (
	tagRE      = regexp.MustCompile(`(?s)<[^>]+>`)
	blankRunRE = regexp.MustCompile(`\n{3,}`)
	spaceRunRE = regexp.MustCompile(`[ \t]{2,}`)
)

// stripTags removes script/style blocks and all remaining tags, unescapes
// the handful of entities that actually show up in lyrics/tab text, and
// collapses excess blank lines. It is not a full HTML parser — good enough
// for "get readable text out of a page", not for precise DOM extraction.
func stripTags(htmlStr string) string {
	s := htmlStr
	for _, tag := range []string{"script", "style", "noscript", "svg"} {
		re := regexp.MustCompile(`(?is)<` + tag + `\b[^>]*>.*?</` + tag + `>`)
		s = re.ReplaceAllString(s, "")
	}
	s = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`(?i)</p>|</div>`).ReplaceAllString(s, "\n")
	s = tagRE.ReplaceAllString(s, "")
	s = unescapeEntities(s)
	s = spaceRunRE.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, strings.TrimRight(l, " \t"))
	}
	s = strings.Join(out, "\n")
	s = blankRunRE.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

var entities = map[string]string{
	"&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": `"`, "&#39;": "'", "&apos;": "'",
	"&nbsp;": " ", "&#8217;": "’", "&#8216;": "‘", "&#8220;": "“", "&#8221;": "”",
}

var numericEntityRE = regexp.MustCompile(`&#x([0-9a-fA-F]+);|&#([0-9]+);`)

func unescapeEntities(s string) string {
	for k, v := range entities {
		s = strings.ReplaceAll(s, k, v)
	}
	s = numericEntityRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := numericEntityRE.FindStringSubmatch(m)
		var n int64
		if sub[1] != "" {
			n, _ = strconv.ParseInt(sub[1], 16, 32)
		} else {
			n, _ = strconv.ParseInt(sub[2], 10, 32)
		}
		if n == 0 {
			return m
		}
		return string(rune(n))
	})
	return s
}
