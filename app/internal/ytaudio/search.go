package ytaudio

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Candidate is one YouTube search result, scored as a reference
// recording for a song: the artist's own upload of the studio version
// first, live takes, covers, karaoke and lessons last.
type Candidate struct {
	ID       string  `json:"id"`
	URL      string  `json:"url"`
	Title    string  `json:"title"`
	Channel  string  `json:"channel"`
	Duration float64 `json:"duration"` // seconds, 0 when unknown
	Views    int64   `json:"views"`
	Score    float64 `json:"score"`
	Official bool    `json:"official"` // the artist's channel, its "- Topic" or VEVO channel
	Note     string  `json:"note,omitempty"`
}

// Search asks yt-dlp for YouTube's first results for artist and title
// (no download, no API key) and returns them best first.
func Search(ctx context.Context, title, artist string) ([]Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	q := strings.TrimSpace(artist + " " + title)
	cmd := exec.CommandContext(ctx, "yt-dlp", "--flat-playlist", "-J", "--no-warnings", "ytsearch12:"+q)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp search: %w", err)
	}
	return parseSearch(out, title, artist)
}

func parseSearch(out []byte, title, artist string) ([]Candidate, error) {
	var res struct {
		Entries []struct {
			ID        string  `json:"id"`
			Title     string  `json:"title"`
			Channel   string  `json:"channel"`
			Uploader  string  `json:"uploader"`
			Duration  float64 `json:"duration"`
			ViewCount int64   `json:"view_count"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("yt-dlp search: %w", err)
	}
	var list []Candidate
	for _, e := range res.Entries {
		if e.ID == "" {
			continue
		}
		ch := e.Channel
		if ch == "" {
			ch = e.Uploader
		}
		c := Candidate{
			ID: e.ID, URL: "https://www.youtube.com/watch?v=" + e.ID,
			Title: e.Title, Channel: ch, Duration: e.Duration, Views: e.ViewCount,
		}
		score(&c, title, artist)
		list = append(list, c)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Score > list[j].Score })
	return list, nil
}

// score rates c: the song's title in the video's, the artist's own
// channel, "official" wording, a song-like length; live versions,
// covers, karaoke, lyric videos and lessons are pushed down (a lyric
// video is often the studio recording, so only slightly).
func score(c *Candidate, title, artist string) {
	vt := fold(c.Title)
	ch := fold(c.Channel)
	a := fold(artist)
	s := 0.0
	words := strings.Fields(fold(title))
	hit := 0
	for _, w := range words {
		if strings.Contains(vt, w) {
			hit++
		}
	}
	if len(words) > 0 {
		s += 3 * float64(hit) / float64(len(words))
	}
	if a != "" {
		// Channels write the artist with or without spaces:
		// "GaryMooreOfficial", "CelineDionVEVO".
		cc, ac := strings.ReplaceAll(ch, " ", ""), strings.ReplaceAll(a, " ", "")
		fan := strings.Contains(ch, "society") || strings.Contains(ch, "fan") || strings.Contains(ch, "tribute")
		switch {
		case cc == ac || cc == ac+"-topic" || cc == ac+"vevo" || cc == ac+"official" || cc == "official"+ac:
			c.Official = true
			s += 3
		case strings.Contains(cc, ac) && !fan:
			c.Official = true
			s += 2
		case strings.Contains(vt, a):
			s += 0.5
		}
	}
	for _, w := range []string{"official video", "official music video", "official audio", "clip officiel", "audio officiel", "vidéo officielle", "official"} {
		if strings.Contains(vt, fold(w)) {
			s += 1
			break
		}
	}
	type penalty struct {
		words []string
		cost  float64
		note  string
	}
	for _, p := range []penalty{
		{[]string{"karaoke", "instrumental", "backing track", "playback"}, 4, "karaoke or instrumental"},
		{[]string{"cover", "reprise par", "tribute"}, 3, "a cover"},
		{[]string{"lesson", "tutorial", "how to play", "cours de", "tuto"}, 4, "a lesson"},
		{[]string{"reaction", "react"}, 4, "a reaction video"},
		{[]string{"live", "concert", "en direct", "unplugged"}, 1.5, "a live version"},
		{[]string{"lyrics", "lyric video", "paroles"}, 0.5, "a lyric video"},
		{[]string{"remix", "sped up", "slowed", "8d audio", "nightcore"}, 3, "a remix"},
	} {
		for _, w := range p.words {
			if containsWord(vt, fold(w)) && !strings.Contains(fold(title), fold(w)) {
				s -= p.cost
				if c.Note == "" {
					c.Note = p.note
				}
				break
			}
		}
	}
	if c.Duration > 0 && (c.Duration < 90 || c.Duration > 600) {
		s -= 2
	}
	c.Score = s
}

// fold lowercases and strips accents and punctuation, for matching
// "Céline Dion" against "celine dion".
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-':
			b.WriteRune('-')
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// containsWord: w in s on word boundaries, so "live" doesn't match
// "deliver".
func containsWord(s, w string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], w)
		if j < 0 {
			return false
		}
		j += i
		before := j == 0 || s[j-1] == ' '
		after := j+len(w) == len(s) || s[j+len(w)] == ' '
		if before && after {
			return true
		}
		i = j + 1
	}
}
