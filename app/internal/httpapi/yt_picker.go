package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"app/internal/store"
	"app/internal/ytaudio"
)

// The YouTube picker (static/js/yt-picker.js, the "yt-picker" partial in
// yt_picker.html): finding a song's video and using it — the Reference
// view's ("+ Reference", "Change the video"), the Import wizard's
// Recording step, Add / Create's reference video. One API for all of
// them:
//
//	GET /api/youtube/find?title=&artist=  YouTube's results for a song, best first
//	GET /api/youtube/find?q=              the same for what was typed ("Title — Artist", or words)
//	GET /api/youtube/find?url=            a pasted link: that one video, its title and channel
//
// The search is yt-dlp's (ytaudio.Search: no API key, no download); a
// link is read with oEmbed (RefTitles), as quick add does. The answer is
// {candidates: [{id, url, title, channel, duration, official, note,
// thumb}]}; saving the one picked is the caller's (the reference: PUT
// /api/songs/{id}/meta, with The Big Songbook's rules).
//
// GET /api/import/youtube/find, the wizard's older address, is the same.

// YouTubeSearch is the picker's search (ytaudio.Search); tests set their
// own.
var YouTubeSearch = ytaudio.Search

// maxPickResults is the most results the picker shows.
const maxPickResults = 8

// pickCandidate is one video the picker shows: the search's candidate,
// with its thumbnail.
type pickCandidate struct {
	ytaudio.Candidate
	Thumb string `json:"thumb,omitempty"`
}

// ytThumb is a YouTube video's thumbnail (16:9, 320×180).
func ytThumb(id string) string {
	if id == "" {
		return ""
	}
	return "https://i.ytimg.com/vi/" + url.PathEscape(id) + "/mqdefault.jpg"
}

func (s *Server) handleYouTubeFind(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if link := strings.TrimSpace(q.Get("url")); link != "" {
		c, ok := pickLink(r.Context(), link)
		if !ok {
			httpError(w, http.StatusBadRequest, "not a web link")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"candidates": []pickCandidate{c}})
		return
	}
	title, artist := strings.TrimSpace(q.Get("title")), strings.TrimSpace(q.Get("artist"))
	if title == "" {
		title, artist = songOfText(q.Get("q"))
	}
	if title == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	list, err := YouTubeSearch(r.Context(), title, artist)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	if len(list) > maxPickResults {
		list = list[:maxPickResults]
	}
	out := make([]pickCandidate, 0, len(list))
	for _, c := range list {
		out = append(out, pickCandidate{Candidate: c, Thumb: ytThumb(c.ID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": out})
}

// pickLink is a pasted link as the picker shows it: a YouTube video by
// its canonical address, its id and thumbnail; any other page as it is.
// Its title (and a video's channel) are oEmbed's or the page's, when
// they come in time; false for what isn't a web link.
func pickLink(ctx context.Context, link string) (pickCandidate, bool) {
	if !strings.Contains(link, "://") {
		link = "https://" + link
	}
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return pickCandidate{}, false
	}
	c := pickCandidate{Candidate: ytaudio.Candidate{URL: link}}
	if id := store.YouTubeID(link); id != "" {
		c.ID, c.URL, c.Thumb = id, "https://www.youtube.com/watch?v="+id, ytThumb(id)
	}
	if RefTitles != nil {
		ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		info, _ := RefTitles.Info(ctx, c.URL)
		cancel()
		c.Title, c.Channel = strings.TrimSpace(info.Title), strings.TrimSpace(info.Author)
	}
	return c, true
}
