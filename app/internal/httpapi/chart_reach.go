package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"app/internal/chordsym"
)

// Who plays a chart (the edit model, docs/catalogue-edit-review.md step
// 7; store/chart_reach.go), for the editor's note on The Big Songbook's
// song.

// chartReachView is who plays the chart, for the editor's note: the
// counts, and the viewer's places with their keys ("A", or the song's own
// key).
type chartReachView struct {
	Songbooks, Setlists, Arrangements int
	Mine                              []chartPlaceView
}

type chartPlaceView struct {
	Name, Owner, Key string
	Link             string
}

func (s *Server) chartReachView(r *http.Request, songID, personID int64, songKey string) *chartReachView {
	reach, err := s.store.ChartReachOf(r.Context(), songID, personID)
	if err != nil {
		return nil
	}
	v := &chartReachView{Songbooks: reach.Songbooks, Setlists: reach.Setlists, Arrangements: reach.Arrangements}
	for _, p := range reach.Mine {
		key := chordsym.KeyIn(songKey, p.Semitones)
		if key == "" && p.Semitones != 0 {
			key = strings.Replace(fmt.Sprintf("%+d", p.Semitones), "-", "−", 1)
		}
		v.Mine = append(v.Mine, chartPlaceView{Name: p.Name, Owner: p.Owner, Key: key, Link: fmt.Sprintf("/songbooks/%d", p.SongbookID)})
	}
	return v
}
