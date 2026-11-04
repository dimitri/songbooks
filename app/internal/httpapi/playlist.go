package httpapi

import (
	"net/http"
)

// playlistRow is the flattened (no Set grouping — a playlist plays straight
// through) per-song row both playlist pages below share.
type playlistRow struct {
	Book, Stem, Title, Tempo, Key, Time string
	HasAudio                            bool
}

// flattenProjectSongs is projectGroup's own per-group loop, flattened
// across every group — a playlist has no use for the Set 1/Set 2
// grouping handleProject's own page shows, just the one running order.
func (s *Server) flattenProjectSongs(book string) []playlistRow {
	groups, err := s.store.ListSongsOrdered(book)
	if err != nil {
		return nil
	}
	var out []playlistRow
	for _, g := range groups {
		for _, stem := range g.Stems {
			row := playlistRow{Book: book, Stem: stem, Title: stem}
			sg, _, err := s.store.LoadSong(book, stem)
			if err != nil {
				continue
			}
			row.Title = orDefault(sg.Get("title"), stem)
			row.Tempo = sg.Get("tempo")
			row.Key = sg.Get("key")
			row.Time = orDefault(sg.Get("time"), "4/4")
			out = append(out, row)
		}
	}
	return out
}

// handleMetronomePlaylist is GET /b/{book}/playlist/metronome — steps
// through every song in the project's own order, each row launching the
// real /metronome page already pre-filled with that song's own bpm/time/
// title (same deep link play.html's own sidebar "Metronome" row already
// uses for one song, just offered for every song in order here instead
// of requiring a detour back through the project page between each).
func (s *Server) handleMetronomePlaylist(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	book := r.PathValue("book")
	rows := s.flattenProjectSongs(book)
	s.render(w, "metronome-playlist", map[string]any{
		"Me": person, "Book": book, "Rows": rows,
	})
}

// handleMP3Playlist is GET /b/{book}/playlist/mp3 — plays every song's
// reference audio back to back, in the project's own order. Prefers the
// "band" category over "youtube" when a song has both (the band's own
// recording is the more useful one to rehearse against back to back);
// falls back to whichever one category exists, and is simply skipped
// (not shown as a dead row) when neither does.
func (s *Server) handleMP3Playlist(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	book := r.PathValue("book")
	rows := s.flattenProjectSongs(book)
	var out []playlistRow
	for _, row := range rows {
		variantID, _, err := s.store.VariantLookup(r.Context(), row.Book, row.Stem)
		if err != nil || variantID == 0 {
			continue
		}
		list, err := s.store.ListSongAudio(r.Context(), variantID)
		if err != nil || len(list) == 0 {
			continue
		}
		row.HasAudio = true
		out = append(out, row)
	}
	s.render(w, "mp3-playlist", map[string]any{
		"Me": person, "Book": book, "Rows": out,
	})
}
