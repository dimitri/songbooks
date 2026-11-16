package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"app/internal/store"
)

// playlistRow is the flattened (no Set grouping — a playlist plays straight
// through) per-song row both playlist pages below share.
type playlistRow struct {
	VariantID                   int64
	Title, Tempo, Key, Time string
	HasAudio                     bool
}

// flattenSongbookSongs is songbookGroup's own per-group loop, flattened
// across every group — a playlist has no use for the Set 1/Set 2
// grouping the songbook page itself shows, just the one running order.
func (s *Server) flattenSongbookSongs(ctx context.Context, songbookID int64) []playlistRow {
	groups, err := s.store.ListSongsOrdered(ctx, songbookID)
	if err != nil {
		return nil
	}
	var out []playlistRow
	for _, g := range groups {
		for _, vid := range g.VariantIDs {
			row := playlistRow{VariantID: vid, Title: "untitled"}
			sg, _, err := s.store.LoadVariant(vid)
			if err != nil {
				continue
			}
			row.Title = orDefault(sg.Get("title"), "untitled")
			row.Tempo = sg.Get("tempo")
			row.Key = sg.Get("key")
			row.Time = orDefault(sg.Get("time"), "4/4")
			out = append(out, row)
		}
	}
	return out
}

// handleMetronomePlaylist is GET /songbooks/{id}/playlist/metronome —
// steps through every song in the songbook's own order, each row
// launching the real /metronome page already pre-filled with that
// song's own bpm/time/title.
func (s *Server) handleMetronomePlaylist(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	person := personFromContext(r)
	if ok, err := s.store.CanAccessSongbook(r.Context(), person.ID, id); err != nil || !ok {
		http.NotFound(w, r)
		return
	}
	rows := s.flattenSongbookSongs(r.Context(), id)
	s.render(w, "metronome-playlist", map[string]any{
		"Me": person, "SongbookID": id, "Rows": rows,
	})
}

// handleMP3Playlist is GET /songbooks/{id}/playlist/mp3 — plays every
// song's reference audio back to back, in the songbook's own order.
// Prefers the "band" category over "youtube" when a song has both (the
// band's own recording is the more useful one to rehearse against back
// to back); falls back to whichever one category exists, and is simply
// skipped (not shown as a dead row) when neither does.
func (s *Server) handleMP3Playlist(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	person := personFromContext(r)
	if ok, err := s.store.CanAccessSongbook(r.Context(), person.ID, id); err != nil || !ok {
		http.NotFound(w, r)
		return
	}
	rows := s.flattenSongbookSongs(r.Context(), id)
	var out []playlistRow
	for _, row := range rows {
		list, err := s.store.ListSongAudio(r.Context(), row.VariantID)
		if err != nil || len(list) == 0 {
			continue
		}
		row.HasAudio = true
		out = append(out, row)
	}
	s.render(w, "mp3-playlist", map[string]any{
		"Me": person, "SongbookID": id, "Rows": out,
	})
}

// playlistTrack is one song of a songbook's playlist (playlist.html):
// what plays for it — the band's own recording, else the reference audio
// taken from YouTube, else the reference video itself — and its sections,
// with where each starts in that recording when its markers say so.
type playlistTrack struct {
	VariantID   int64             `json:"id"` // 0 for a lesson
	Num         string            `json:"num"`
	Title       string            `json:"title"`
	Subtitle    string            `json:"subtitle"`
	Artist      string            `json:"artist"`
	Key         string            `json:"key"`
	Tempo       string            `json:"tempo"`
	Href        string            `json:"href"`
	Source      string            `json:"source"` // "band", "youtube", "video" or "": nothing to play
	SourceLabel string            `json:"sourceLabel"`
	Category    string            `json:"category,omitempty"`
	AudioURL    string            `json:"audio,omitempty"`
	VideoID     string            `json:"video,omitempty"`
	Sections    []playlistSection `json:"sections"`
	both        bool              // has both its own and a reference recording
}

// playlistSection is a section of a track: its name, its length in bars
// (how wide it shows), and its start in the recording (-1: unmarked).
type playlistSection struct {
	Label string  `json:"label"`
	Bars  int     `json:"bars"`
	Start float64 `json:"start"`
}

// youtubeID is store.YouTubeID.
func youtubeID(raw string) string { return store.YouTubeID(raw) }

// playlistTrackFor is variantID's track; prefer is "reference" to play the
// reference recording before the band's own.
func (s *Server) playlistTrackFor(ctx context.Context, songbookID, variantID int64, prefer string) (playlistTrack, bool) {
	sg, _, err := s.store.LoadVariant(variantID)
	if err != nil {
		return playlistTrack{}, false
	}
	t := playlistTrack{
		VariantID: variantID, Title: orDefault(sg.Get("title"), "untitled"), Artist: sg.Get("artist"),
		Key: sg.Get("key"), Tempo: sg.Get("tempo"),
		Href: fmt.Sprintf("/song/%d?songbook=%d", variantID, songbookID),
	}
	audio, _ := s.store.ListSongAudio(ctx, variantID)
	byCat := map[store.AudioCategory]store.SongAudio{}
	for _, a := range audio {
		byCat[a.Category] = a
	}
	order := []store.AudioCategory{store.AudioBand, store.AudioYouTube}
	if prefer == "reference" {
		order = []store.AudioCategory{store.AudioYouTube, store.AudioBand}
	}
	_, hasBand := byCat[store.AudioBand]
	_, hasRef := byCat[store.AudioYouTube]
	t.both = hasBand && hasRef
	var chosen *store.SongAudio
	for _, c := range order {
		if a, ok := byCat[c]; ok {
			chosen = &a
			break
		}
	}
	vid := youtubeID(sg.Get("reference"))
	switch {
	case chosen != nil:
		t.Source, t.Category = string(chosen.Category), string(chosen.Category)
		t.AudioURL = fmt.Sprintf("/song/%d/audio.mp3?category=%s&v=%d", variantID, chosen.Category, chosen.ByteSize)
		t.SourceLabel = "Reference"
		if chosen.Category == store.AudioBand {
			t.SourceLabel = "Ours"
		}
	case vid != "":
		t.Source, t.VideoID, t.SourceLabel = "video", vid, "Video"
	}
	starts := map[int]float64{}
	if chosen != nil {
		if marks, err := s.store.ListAudioMarkers(ctx, chosen.ID); err == nil {
			for _, m := range marks {
				starts[m.SectionIndex] = m.TimeSeconds
			}
		}
	}
	for _, sec := range buildSections(sg) {
		label := sec.Label
		if sec.LabelNumber != 0 {
			label = fmt.Sprintf("%s %d", label, sec.LabelNumber)
		}
		start, ok := starts[sec.Index]
		if !ok {
			start = -1
		}
		t.Sections = append(t.Sections, playlistSection{Label: label, Bars: max(sec.BarCount, 1), Start: start})
	}
	return t, true
}

// handlePlaylistPage is GET /songbooks/{id}/playlist: the songbook's songs
// played back to back, in its order — each song's recording, or its
// reference video — to listen to in the car or around the house. On a
// tablet or a computer, the song playing shows beside the player, its
// map following the recording. ?prefer=reference plays the reference
// recordings before the band's own.
func (s *Server) handlePlaylistPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	person := personFromContext(r)
	if ok, err := s.canSeeSongbook(r, id); err != nil || !ok {
		http.NotFound(w, r)
		return
	}
	sb, err := s.store.GetSongbook(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	prefer := r.URL.Query().Get("prefer")
	var tracks, silent []playlistTrack
	choice := false // some song has both recordings: which plays is a choice
	if sb.IsCourse() {
		// A course plays its lessons' videos, in order.
		tracks, silent = s.coursePlaylist(r.Context(), sb)
	}
	for _, row := range s.flattenSongbookSongs(r.Context(), id) {
		if sb.IsCourse() {
			break
		}
		t, ok := s.playlistTrackFor(r.Context(), id, row.VariantID, prefer)
		if !ok {
			continue
		}
		choice = choice || t.both
		t.Num = strconv.Itoa(len(tracks) + len(silent) + 1)
		t.Subtitle = strings.Join(nonEmpty(t.Artist, t.Key), " · ")
		if t.Source == "" {
			silent = append(silent, t)
		} else {
			tracks = append(tracks, t)
		}
	}
	back, owner := fmt.Sprintf("/songbooks/%d", id), sb.Name
	switch sb.OwnerKind {
	case "band":
		if b, err := s.store.GetBand(r.Context(), sb.OwnerBandID); err == nil {
			owner = b.Name
			if sb.IsDefault {
				back = fmt.Sprintf("/bands/%d/catalog", b.ID)
			}
		}
	case "community":
		if c, err := s.store.GetCommunity(r.Context(), sb.OwnerCommunityID); err == nil {
			owner = c.Name
		}
	}
	tracksJSON, _ := json.Marshal(tracks)
	s.render(w, "playlist", map[string]any{
		"Me": person, "Songbook": sb, "Owner": owner, "Back": back,
		"Tracks": tracks, "Silent": silent, "TracksJSON": template.JS(tracksJSON),
		"PreferReference": prefer == "reference", "Choice": choice, "VideoFirst": sb.IsCourse(),
	})
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// coursePlaylist is a course's lessons as a playlist: each lesson's video
// (lessonVideo), else the first recording one of its songs has.
func (s *Server) coursePlaylist(ctx context.Context, sb *store.Songbook) (tracks, silent []playlistTrack) {
	lessons, _, err := s.store.ListLessons(ctx, sb.ID)
	if err != nil {
		return nil, nil
	}
	for _, l := range lessons {
		t := playlistTrack{Title: l.Title, Href: lessonHref(sb.ID, l.ID), Artist: sb.Name, Sections: []playlistSection{}}
		if l.Number != nil {
			t.Num = strconv.Itoa(*l.Number)
		}
		var titles []string
		for _, song := range l.Songs {
			titles = append(titles, song.Title)
		}
		t.Subtitle = strings.Join(titles, " · ")
		if vid := s.lessonVideo(l); vid != "" {
			t.Source, t.VideoID, t.SourceLabel = "video", vid, "Video"
		} else {
			for _, song := range l.Songs {
				if st, ok := s.playlistTrackFor(ctx, sb.ID, song.VariantID, ""); ok && st.AudioURL != "" {
					t.Source, t.Category, t.AudioURL, t.SourceLabel = st.Source, st.Category, st.AudioURL, st.SourceLabel
					break
				}
			}
		}
		if t.Source == "" {
			silent = append(silent, t)
		} else {
			tracks = append(tracks, t)
		}
	}
	return tracks, silent
}
