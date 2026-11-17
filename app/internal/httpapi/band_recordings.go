package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"app/internal/store"
)

// A band's recordings (store/band_recordings.go, docs/band-recordings.md):
// its Rehearsals and Concerts tabs, each a calendar of the days it
// recorded, a day's recordings in the order played, a player streaming
// them from the band's pCloud folder, everyone's stars; a song's page of
// recordings, the best rated first, to choose the band's reference; and
// the band's corrections to what its file names say.

// recDay is a day of a recordings calendar.
type recDay struct {
	Date     time.Time
	InMonth  bool
	Session  bool
	Selected bool
	Href     string
}

// recMonth is a month of a recordings calendar, Monday first.
type recMonth struct {
	Title      string
	Prev, Next string // their links, "" past the first or last session
	Weeks      [][]recDay
}

func buildRecMonth(base string, first time.Time, sessions []store.RecordingSession, selected time.Time) recMonth {
	has := map[string]bool{}
	var earliest, latest time.Time
	for i, s := range sessions {
		has[s.Date.Format("2006-01-02")] = true
		if i == 0 || s.Date.Before(earliest) {
			earliest = s.Date
		}
		if i == 0 || s.Date.After(latest) {
			latest = s.Date
		}
	}
	m := recMonth{Title: first.Format("January 2006")}
	if prev := first.AddDate(0, -1, 0); !earliest.IsZero() && !prev.AddDate(0, 1, 0).Before(earliest) {
		m.Prev = fmt.Sprintf("%s?month=%s", base, prev.Format("2006-01"))
	}
	if next := first.AddDate(0, 1, 0); !next.After(latest) {
		m.Next = fmt.Sprintf("%s?month=%s", base, next.Format("2006-01"))
	}
	start := first.AddDate(0, 0, -((int(first.Weekday()) + 6) % 7))
	end := first.AddDate(0, 1, 0)
	var week []recDay
	for d := start; d.Before(end) || len(week) > 0; d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		day := recDay{Date: d, InMonth: d.Month() == first.Month(), Session: has[key], Selected: key == selected.Format("2006-01-02")}
		if day.Session {
			day.Href = fmt.Sprintf("%s?date=%s", base, key)
		}
		week = append(week, day)
		if len(week) == 7 {
			m.Weeks = append(m.Weeks, week)
			week = nil
		}
	}
	return m
}

// bandForRecordings is the request's band; it answers the request itself
// when there's none.
func (s *Server) bandForRecordings(w http.ResponseWriter, r *http.Request) (*store.Band, bool) {
	bandID, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return band, true
}

// handleBandRehearsalsPage is GET /bands/{id}/rehearsals.
func (s *Server) handleBandRehearsalsPage(w http.ResponseWriter, r *http.Request) {
	s.bandRecordingsPage(w, r, "rehearsal")
}

// handleBandConcertsPage is GET /bands/{id}/concerts.
func (s *Server) handleBandConcertsPage(w http.ResponseWriter, r *http.Request) {
	s.bandRecordingsPage(w, r, "concert")
}

// bandRecordingsPage is a band's rehearsals or concerts: ?date= a day's
// recordings (the latest by default), ?month= the calendar's month.
func (s *Server) bandRecordingsPage(w http.ResponseWriter, r *http.Request, kind string) {
	band, ok := s.bandForRecordings(w, r)
	if !ok {
		return
	}
	person := personFromContext(r)
	tab := map[string]string{"rehearsal": "rehearsals", "concert": "concerts"}[kind]
	base := fmt.Sprintf("/bands/%d/%s", band.ID, tab)
	data := map[string]any{"Me": person, "Band": band, "Kind": kind, "Tab": tab, "Base": base}
	src, err := s.store.GetBandRecordingSource(r.Context(), band.ID)
	if errors.Is(err, store.ErrNotFound) {
		s.render(w, "band_recordings", data)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data["Source"] = src
	sessions, err := s.store.ListRecordingSessions(r.Context(), band.ID, kind)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data["Sessions"] = sessions
	questions, err := s.store.ListDateQuestions(r.Context(), band.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	unmatched, err := s.store.UnmatchedTitles(r.Context(), band.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data["Questions"], data["Unmatched"] = len(questions), len(unmatched)

	var selected time.Time
	if d, err := time.Parse("2006-01-02", r.URL.Query().Get("date")); err == nil {
		selected = d
	} else if len(sessions) > 0 {
		selected = sessions[0].Date
	}
	if !selected.IsZero() {
		recs, err := s.store.SessionRecordings(r.Context(), band.ID, person.ID, kind, selected)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data["Selected"], data["Recordings"] = selected, recs
		for i, ses := range sessions {
			if ses.Date.Equal(selected) {
				if i+1 < len(sessions) {
					data["Older"] = fmt.Sprintf("%s?date=%s", base, sessions[i+1].Date.Format("2006-01-02"))
				}
				if i > 0 {
					data["Newer"] = fmt.Sprintf("%s?date=%s", base, sessions[i-1].Date.Format("2006-01-02"))
				}
			}
		}
	}
	month := selected
	if m, err := time.Parse("2006-01", r.URL.Query().Get("month")); err == nil {
		month = m
	}
	if month.IsZero() {
		month = time.Now()
	}
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	data["Month"] = buildRecMonth(base, first, sessions, selected)

	if kind == "rehearsal" {
		stats, err := s.store.BandPracticeStats(r.Context(), band.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data["Stats"] = practiceSummary(stats, len(sessions))
	}
	s.render(w, "band_recordings", data)
}

// practiceView is the Rehearsals tab's stats.
type practiceView struct {
	Rehearsals int
	Most       []store.SongPracticeStats // the most played
	Least      []store.SongPracticeStats // played, the least
	Longest    []store.SongPracticeStats // not played for the longest
	Never      []store.SongPracticeStats // never recorded at a rehearsal
	Best       []store.SongPracticeStats // the best rated
	All        []store.SongPracticeStats
}

func practiceSummary(stats []store.SongPracticeStats, rehearsals int) practiceView {
	v := practiceView{Rehearsals: rehearsals, All: stats}
	var played []store.SongPracticeStats
	for _, s := range stats {
		if s.Sessions > 0 {
			played = append(played, s)
		} else {
			v.Never = append(v.Never, s)
		}
	}
	top := func(list []store.SongPracticeStats, less func(a, b store.SongPracticeStats) bool, n int) []store.SongPracticeStats {
		out := append([]store.SongPracticeStats(nil), list...)
		sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
		if len(out) > n {
			out = out[:n]
		}
		return out
	}
	v.Most = top(played, func(a, b store.SongPracticeStats) bool { return a.Sessions > b.Sessions }, 5)
	v.Least = top(played, func(a, b store.SongPracticeStats) bool { return a.Sessions < b.Sessions }, 5)
	v.Longest = top(played, func(a, b store.SongPracticeStats) bool { return a.Last.Before(*b.Last) }, 5)
	var rated []store.SongPracticeStats
	for _, s := range played {
		if s.Stars > 0 {
			rated = append(rated, s)
		}
	}
	v.Best = top(rated, func(a, b store.SongPracticeStats) bool { return a.Stars > b.Stars }, 5)
	return v
}

// handleBandSongRecordingsPage is GET /bands/{id}/recordings/songs/{variantID}:
// a song's recordings, the best rated first, to choose the band's reference.
func (s *Server) handleBandSongRecordingsPage(w http.ResponseWriter, r *http.Request) {
	band, ok := s.bandForRecordings(w, r)
	if !ok {
		return
	}
	variantID, err := pathID(r, "variantID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	person := personFromContext(r)
	src, err := s.store.GetBandRecordingSource(r.Context(), band.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	recs, err := s.store.SongRecordings(r.Context(), band.ID, person.ID, variantID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	variant, err := s.store.GetVariant(r.Context(), variantID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, "band_song_recordings", map[string]any{
		"Me": person, "Band": band, "Source": src, "Song": variant, "Recordings": recs,
	})
}

// handleBandRecordingFixesPage is GET /bands/{id}/recordings/fixes: the
// band's corrections, and what its recordings' names leave to answer.
func (s *Server) handleBandRecordingFixesPage(w http.ResponseWriter, r *http.Request) {
	band, ok := s.bandForRecordings(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	questions, err := s.store.ListDateQuestions(ctx, band.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	unmatched, err := s.store.UnmatchedTitles(ctx, band.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	fixes, err := s.store.ListRecordingFixes(ctx, band.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	songs, err := s.store.BandPracticeStats(ctx, band.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sort.SliceStable(songs, func(i, j int) bool { return songs[i].Title < songs[j].Title })
	names, err := s.store.NameReport(ctx, band.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	own, err := s.store.NameTemplates(ctx, band.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The files no song is found for: the named ones, or (?raw=1) the
	// recorder's own.
	showRaw := r.URL.Query().Get("raw") == "1"
	files, err := s.store.UnmatchedFiles(ctx, band.ID, showRaw)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "band_recording_fixes", map[string]any{
		"Me": personFromContext(r), "Band": band, "Questions": questions, "Unmatched": unmatched, "Fixes": fixes, "Songs": songs,
		"Names": names, "Own": own, "Files": files, "ShowRaw": showRaw,
	})
}

// handleSetBandRecordingSource is POST /api/bands/{id}/recordings/source:
// {"link": a pCloud public link}; the folder is listed straight away.
func (s *Server) handleSetBandRecordingSource(w http.ResponseWriter, r *http.Request) {
	bandID, _ := pathID(r, "id")
	var req struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	src, err := s.store.SetBandRecordingSource(r.Context(), bandID, req.Link, personFromContext(r).ID)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": src.Name})
}

// handleSyncBandRecordings is POST /api/bands/{id}/recordings/sync.
func (s *Server) handleSyncBandRecordings(w http.ResponseWriter, r *http.Request) {
	bandID, _ := pathID(r, "id")
	if err := s.store.SyncBandRecordings(r.Context(), bandID); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "the band has no recordings folder")
		return
	} else if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"synced": true})
}

// handleRateBandRecording is PUT /api/bands/{id}/recordings/{recID}/stars:
// {"stars": 3.5}, 0 to take them back.
func (s *Server) handleRateBandRecording(w http.ResponseWriter, r *http.Request) {
	bandID, _ := pathID(r, "id")
	recID, err := pathID(r, "recID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid recording id")
		return
	}
	var req struct {
		Stars float64 `json:"stars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	person := personFromContext(r)
	if err := s.store.RateRecording(r.Context(), bandID, recID, person.ID, req.Stars); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	} else if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	rec, err := s.store.GetBandRecording(r.Context(), bandID, person.ID, recID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stars": rec.Stars, "ratings": rec.Ratings, "mine": rec.MyStars})
}

// handleSetBandSongRecording is PUT /api/bands/{id}/songs/{variantID}/recording:
// {"recording": id} makes it the band's reference for the song, 0 none.
func (s *Server) handleSetBandSongRecording(w http.ResponseWriter, r *http.Request) {
	bandID, _ := pathID(r, "id")
	variantID, err := pathID(r, "variantID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song id")
		return
	}
	var req struct {
		Recording int64 `json:"recording"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetSongRecording(r.Context(), bandID, variantID, req.Recording, personFromContext(r).ID); errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleSetBandRecordingFix is POST /api/bands/{id}/recordings/fixes:
// {"kind": "date", "from": "2024-05-26", "to": "2025-05-26"}, or
// {"kind": "title", "from": "Kawazaki", "to": "<song id>"}.
func (s *Server) handleSetBandRecordingFix(w http.ResponseWriter, r *http.Request) {
	bandID, _ := pathID(r, "id")
	var req struct {
		Kind string `json:"kind"`
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetRecordingFix(r.Context(), bandID, req.Kind, req.From, req.To, personFromContext(r).ID); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleDeleteBandRecordingFix is DELETE /api/bands/{id}/recordings/fixes/{fixID}.
func (s *Server) handleDeleteBandRecordingFix(w http.ResponseWriter, r *http.Request) {
	bandID, _ := pathID(r, "id")
	fixID, err := strconv.ParseInt(r.PathValue("fixID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid correction id")
		return
	}
	if err := s.store.DeleteRecordingFix(r.Context(), bandID, fixID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleSetNameTemplates is PUT /api/bands/{id}/recordings/templates:
// {"templates": ["{n} - {title} - {date}"]}, the band's own, read before
// the built-in ones; the names are read again straight away.
func (s *Server) handleSetNameTemplates(w http.ResponseWriter, r *http.Request) {
	bandID, _ := pathID(r, "id")
	var req struct {
		Templates []string `json:"templates"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetNameTemplates(r.Context(), bandID, req.Templates); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleTryNameTemplate is POST /api/bands/{id}/recordings/templates/try:
// {"template": "..."}, what it would read in the band's file names.
func (s *Server) handleTryNameTemplate(w http.ResponseWriter, r *http.Request) {
	bandID, _ := pathID(r, "id")
	var req struct {
		Template string `json:"template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	try, err := s.store.TryTemplate(r.Context(), bandID, req.Template)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, try)
}
