package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"app/internal/song"
	"app/internal/store"
)

// Quick add (docs/coaching.md, "Adding a song for a student"): as simple
// as finding the video and the lyrics without the app. The coach pastes
// a YouTube link (or types "Title — Artist") in the Add / Create dialog
// (add-song.js): the app reads the video's title and channel, finds the
// song if a songbook may link it already (the community's own version
// first, else the public one: same video, or same title and artist), and
// else creates it with the video as its reference and the singer's
// lyrics fetched; one more click gives it to a student, in a key.
//
//	GET  /api/songbooks/{id}/quick-add?q=  what the input is, and the song found
//	POST /api/songbooks/{id}/quick-add     {songId} links it; {title, artist, reference} creates it

// QuickDraft is the singer's chart for a new song, title and artist (the
// editor's Import, songimport.VocalDraft: the lyrics with their
// sections, no chords in them, iReal Pro's grids where they match);
// cmd/songbooks sets it, nil (tests) fetches nothing.
var QuickDraft func(ctx context.Context, st *store.Store, title, artist string) (string, error)

// quickDraftTimeout is the most a quick add waits for the lyrics: the
// song is made without them after that.
const quickDraftTimeout = 25 * time.Second

// videoNoise is what a video's title adds to the song's: "(Official
// Video)", "[Lyrics]", "(Clip officiel)", "| Official Audio"…
var (
	videoNoise = regexp.MustCompile(`(?i)\s*[(\[][^)\]]*\b(official|video|vid[ée]o|audio|lyrics?|paroles|clip|hd|hq|4k|remaster(ed)?|visuali[sz]er|officiel(le)?|music)\b[^)\]]*[)\]]`)
	videoTail  = regexp.MustCompile(`(?i)\s*[|/]\s*.*$|\s+(official\s+(music\s+)?video|official\s+audio|lyric\s+video|clip\s+officiel|hd|hq|4k)\s*$`)
	channelEnd = regexp.MustCompile(`(?i)\s*(-\s*topic|vevo|official|officiel)\s*$`)
)

// songOfVideo is the song's title and artist from a video's title and
// channel: "Artist - Title (Official Video)", or the title alone on the
// artist's channel ("Artist - Topic", "ArtistVEVO").
func songOfVideo(videoTitle, channel string) (title, artist string) {
	t := videoNoise.ReplaceAllString(videoTitle, "")
	t = strings.TrimSpace(videoTail.ReplaceAllString(t, ""))
	for _, sep := range []string{" - ", " – ", " — ", " -- ", " : "} {
		if i := strings.Index(t, sep); i > 0 {
			return strings.TrimSpace(t[i+len(sep):]), strings.TrimSpace(t[:i])
		}
	}
	return t, strings.TrimSpace(channelEnd.ReplaceAllString(channel, ""))
}

// songOfText is the title and artist a person typed: "Title — Artist"
// (the app's own way, docs/ownership-model.md §3), "Title - Artist",
// "Title by Artist"; the title alone otherwise.
func songOfText(q string) (title, artist string) {
	q = strings.TrimSpace(q)
	for _, sep := range []string{" — ", " – ", " - ", " by ", " par "} {
		if i := strings.Index(q, sep); i > 0 {
			return strings.TrimSpace(q[:i]), strings.TrimSpace(q[i+len(sep):])
		}
	}
	return q, ""
}

func isLink(q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	return strings.HasPrefix(q, "http://") || strings.HasPrefix(q, "https://") || strings.HasPrefix(q, "www.") ||
		strings.HasPrefix(q, "youtu.be/") || strings.HasPrefix(q, "youtube.com/")
}

// quickLookup is what GET quick-add answers.
type quickLookup struct {
	Title      string           `json:"title"`
	Artist     string           `json:"artist"`
	Reference  string           `json:"reference"`
	VideoID    string           `json:"videoId,omitempty"`
	Match      *store.SongMatch `json:"match"`
	InSongbook bool             `json:"inSongbook"`
	// Lyrics: a new song gets its lyrics fetched (QuickDraft is set).
	Lyrics bool `json:"lyrics"`
}

// quickSongbook is the songbook of a quick add, which the person changes.
func (s *Server) quickSongbook(w http.ResponseWriter, r *http.Request) (*store.Songbook, bool) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return nil, false
	}
	if ok, err := s.store.CanEditSongbook(r.Context(), personFromContext(r).ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no edit access to this songbook")
		return nil, false
	}
	sb, err := s.store.GetSongbook(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such songbook")
		return nil, false
	}
	return sb, true
}

func (s *Server) handleGetQuickAdd(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.quickSongbook(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpError(w, http.StatusBadRequest, "missing q")
		return
	}
	out := quickLookup{Lyrics: QuickDraft != nil}
	typed := !isLink(q)
	if typed {
		out.Title, out.Artist = songOfText(q)
	} else {
		if !strings.Contains(q, "://") {
			q = "https://" + q
		}
		out.Reference = q
		out.VideoID = store.YouTubeID(q)
		if out.VideoID != "" {
			out.Reference = "https://www.youtube.com/watch?v=" + out.VideoID
		}
		if RefTitles != nil {
			c, cancel := context.WithTimeout(ctx, 6*time.Second)
			info, err := RefTitles.Info(c, out.Reference)
			cancel()
			if err != nil {
				log.Printf("quick add: %s: %v", out.Reference, err)
			}
			out.Title, out.Artist = songOfVideo(info.Title, info.Author)
		}
	}
	me := personFromContext(r).ID
	m, err := s.store.FindSongMatch(ctx, me, sb.Owner(), out.Title, out.Artist, out.VideoID)
	if err == nil && m == nil && typed && out.Artist != "" {
		// "Artist - Title", typed as a video names it.
		if m, err = s.store.FindSongMatch(ctx, me, sb.Owner(), out.Artist, out.Title, ""); m != nil {
			out.Title, out.Artist = out.Artist, out.Title
		}
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Match = m
	if m != nil {
		_, out.InSongbook, _ = s.store.SongbookEntryKey(ctx, sb.ID, m.ID)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePostQuickAdd(w http.ResponseWriter, r *http.Request) {
	sb, ok := s.quickSongbook(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	me := personFromContext(r).ID
	var req struct {
		SongID    int64  `json:"songId"`
		Title     string `json:"title"`
		Artist    string `json:"artist"`
		Reference string `json:"reference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.SongID != 0 {
		// The song found: linked, as Add does (store.LinkCheck), the
		// community's own version when it has one.
		if !s.writeLinkErr(w, r, s.store.LinkCheck(ctx, me, req.SongID, sb)) {
			return
		}
		id := s.coachingSongFor(ctx, sb, req.SongID)
		if err := s.store.AddToSongbook(ctx, sb.ID, id, me); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"variantId": id, "created": false})
		return
	}
	title, artist, ref := strings.TrimSpace(req.Title), strings.TrimSpace(req.Artist), strings.TrimSpace(req.Reference)
	if title == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	id, err := s.store.CreateSongIn(ctx, me, sb.ID, title, artist, ref, false)
	switch {
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, "no edit access to this songbook")
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lyrics := false
	if QuickDraft != nil {
		c, cancel := context.WithTimeout(ctx, quickDraftTimeout)
		text, err := QuickDraft(c, s.store, title, artist)
		cancel()
		if err != nil {
			log.Printf("quick add: lyrics of %q — %q: %v", title, artist, err)
		} else if strings.TrimSpace(text) != "" {
			sg := song.Parse(text)
			sg.Set("title", title)
			if artist != "" {
				sg.Set("artist", artist)
			}
			if ref != "" {
				sg.Set("reference", ref)
			}
			if _, version, err := s.store.LoadVariant(id); err == nil {
				if _, err := s.store.SaveVariant(id, sg, version, me); err == nil {
					lyrics = true
				} else {
					log.Printf("quick add: saving the lyrics of song %d: %v", id, err)
				}
			}
		}
	}
	if ref != "" {
		s.fillRefTitle(id, ref)
	}
	writeJSON(w, http.StatusOK, map[string]any{"variantId": id, "created": true, "lyrics": lyrics})
}
