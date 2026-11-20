package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"app/internal/gpfacts"
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
// the usual ones; ?back= and ?title= give the way back. The song page
// opens the same tuner in place ("Accorder", play.html), with the same
// tunings (handleSong).
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
				data["SongCapo"] = songCapo(sg.Get("capo"))
			}
			song = songTunings(s.loadSongGP(r.Context(), id).Facts, s.langOf(w))
		}
	}
	data["SongTunings"] = song
	s.render(w, "tuner", data)
}

// songCapo is a song's "capo:" as a fret number, 0 for none.
func songCapo(v string) int {
	capo, _ := strconv.Atoi(strings.TrimSpace(v))
	return capo
}

// songTunings is a song's own tunings, from its Guitar Pro file: one per
// pitched track with a tuning (drums have none), the same strings and
// capo only once, lowest string first; never nil.
func songTunings(facts *gpfacts.Facts, lang string) []tunerTuning {
	song := []tunerTuning{}
	if facts == nil {
		return song
	}
	seen := map[string]bool{}
	for _, t := range facts.Tracks {
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
			name = i18n.T(lang, "Track %d", t.N)
		}
		song = append(song, tunerTuning{Name: name, Notes: notes, Capo: t.Capo})
	}
	return song
}
