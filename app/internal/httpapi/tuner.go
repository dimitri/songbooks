package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"app/internal/i18n"
)

// tunerTuning is a tuning the tuner offers: its name, its strings' open
// notes as MIDI numbers (lowest first), and the capo it's played with.
type tunerTuning struct {
	Name  string `json:"name"`
	Notes []int  `json:"notes"`
	Capo  int    `json:"capo,omitempty"`
}

// handleTunerPage is GET /tuner: a chromatic tuner that listens through
// the microphone (tuner.js; nothing leaves the browser). ?song=ID adds
// that song's tunings from its Guitar Pro file, and its capo, ahead of
// the usual ones; ?back= and ?title= give the way back.
func (s *Server) handleTunerPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	q := r.URL.Query()
	data := map[string]any{"Me": person, "BackURL": q.Get("back"), "BackTitle": q.Get("title")}
	song := []tunerTuning{}
	if id, err := strconv.ParseInt(q.Get("song"), 10, 64); err == nil && id > 0 {
		if ok, err := s.store.CanViewVariant(r.Context(), person.ID, id); err == nil && ok {
			sg, _, err := s.store.LoadVariant(id)
			if err == nil {
				data["SongTitle"] = sg.Get("title")
				capo, _ := strconv.Atoi(strings.TrimSpace(sg.Get("capo")))
				data["SongCapo"] = capo
			}
			if gp := s.loadSongGP(r.Context(), id); gp.Facts != nil {
				seen := map[string]bool{}
				for _, t := range gp.Facts.Tracks {
					if t.Drums || len(t.Tuning) == 0 {
						continue
					}
					notes := append([]int(nil), t.Tuning...)
					sort.Ints(notes) // lowest string first, whatever order the file keeps
					key := fmt.Sprint(t.Capo, notes)
					if seen[key] {
						continue
					}
					seen[key] = true
					name := t.Name
					if name == "" {
						name = i18n.T(s.langOf(w), "Track %d", t.N)
					}
					song = append(song, tunerTuning{Name: name, Notes: notes, Capo: t.Capo})
				}
			}
		}
	}
	data["SongTunings"] = song
	s.render(w, "tuner", data)
}
