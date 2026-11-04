package httpapi

import (
	"context"
	"net/http"
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
