package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"app/internal/audioanalysis"
	"app/internal/chartalign"
	"app/internal/melody"
	"app/internal/song"
	"app/internal/songimport"
	"app/internal/store"
)

// The song page's Melody view (docs/melody-sheet.md): the lead voice's
// notes on a staff, in the song's bars, its lyrics under them.
//
//	GET /api/songs/{id}/melody?transpose=N           the score (alphaTex) and how sure it is
//	GET /api/songs/{id}/melody.musicxml?transpose=N  the same as MusicXML, to download
//	PUT /api/songs/{id}/melody/lyrics-shift          an editor's correction {section, line, shift}
//
// The notes are found in the background, once per split (RunMelodyNotes,
// started by cmd/songbooks); the rest is done here, per request, from the
// song as it is now: its sync with the recording, its bars and lyrics.

// melodyNotesAnalyzer finds a vocals part's notes; audioanalysis's, a
// fake in tests.
type melodyNotesAnalyzer func(ctx context.Context, vocals []byte) (audioanalysis.MelodyNotes, bool, error)

// RunMelodyNotes finds the notes not found yet, until ctx ends, one
// vocals part at a time, the newest first.
func RunMelodyNotes(ctx context.Context, st *store.Store) {
	for {
		did, err := findMelodyNotes(ctx, st, audioanalysis.AnalyzeMelodyNotes)
		if err != nil {
			log.Printf("melody notes: %v", err)
		}
		if did && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(melodyIdle):
		}
	}
}

// findMelodyNotes finds one recording's notes: false when there was none
// to find. A part that can't be read is kept as "no melody", so it isn't
// tried again until it's split again.
func findMelodyNotes(ctx context.Context, st *store.Store, analyze melodyNotesAnalyzer) (bool, error) {
	j, err := st.NextMelodyNotes(ctx, audioanalysis.MelodyNotesAnalyzer)
	if err != nil || j == nil {
		return false, err
	}
	start := time.Now()
	vocals, _, _, err := st.GetStemData(ctx, j.SongAudioID, "vocals")
	if err != nil {
		return false, err
	}
	actx, cancel := context.WithTimeout(ctx, audioanalysis.DecodeTimeout)
	m, ok, err := analyze(actx, vocals)
	cancel()
	if err != nil {
		log.Printf("melody notes of recording %d: %v", j.SongAudioID, err)
		ok = false
	}
	took := time.Since(start).Seconds()
	if err := st.SaveMelodyNotes(ctx, *j, audioanalysis.MelodyNotesAnalyzer, ok, m.Notes, m.Voiced, took); err != nil {
		return false, err
	}
	log.Printf("melody notes of recording %d: %v, %d notes (%.0f s sung), in %.1f s", j.SongAudioID, ok, len(m.Notes), m.Voiced, took)
	return true, nil
}

// melodyLine is a lyric line as the page shows it (the corrections).
type melodyLine struct {
	Key     string  `json:"key"`
	Section int     `json:"section"`
	Line    int     `json:"line"`
	Label   string  `json:"label"`
	Text    string  `json:"text"`
	Matched bool    `json:"matched"`
	Conf    float64 `json:"conf"`
	Shift   int     `json:"shift"`
	// Bar: the score's bar its first note is in (-1: not placed).
	Bar int `json:"bar"`
}

type melodyResponse struct {
	// State: "ready"; else why there's no melody yet: "none" (no
	// recording), "split" (its parts aren't there yet), "notes" (they're
	// being found), "unsung" (too little singing), "sync" (no bars in the
	// recording yet).
	State    string `json:"state"`
	Category string `json:"category,omitempty"`
	AlphaTex string `json:"alphaTex,omitempty"`
	// Bars: the played bar of each of the score's bars.
	Bars  []int             `json:"bars,omitempty"`
	Marks []melody.BeatMark `json:"marks,omitempty"`
	Lines []melodyLine      `json:"lines,omitempty"`
	// SyncSource: "points", "markers" or "analysis" (estimated from the
	// recording's beats, nothing saved); SyncEstimated: a program placed
	// it and nobody checked it.
	SyncSource    string  `json:"syncSource,omitempty"`
	SyncEstimated bool    `json:"syncEstimated,omitempty"`
	Confidence    float64 `json:"confidence"`
	Notes         int     `json:"notes"`
	LowNotes      int     `json:"lowNotes"`
	Dropped       int     `json:"dropped"`
	Lang          string  `json:"lang,omitempty"`
}

// melodyScore builds the song's melody; resp.State says why there's none.
func (s *Server) melodyScore(ctx context.Context, variantID int64, transpose int) (melodyResponse, *melody.Score, error) {
	var resp melodyResponse
	m, err := s.store.GetSongMelody(ctx, variantID, audioanalysis.MelodyNotesAnalyzer)
	if err != nil {
		return resp, nil, err
	}
	switch {
	case m == nil:
		resp.State = "none"
		return resp, nil, nil
	case !m.Split:
		resp.State = "split"
	case !m.Found:
		resp.State = "notes"
	case !m.OK:
		resp.State = "unsung"
	}
	resp.Category = string(m.Category)
	if resp.State != "" {
		return resp, nil, nil
	}
	title, _, _, body, err := s.store.VariantContent(ctx, variantID)
	if err != nil {
		return resp, nil, err
	}
	sg := song.Parse(body)
	secs := buildSections(sg)
	an, err := s.store.GetAnalysis(ctx, m.AudioID)
	if err != nil {
		return resp, nil, err
	}
	g, source, estimated, err := s.melodyGrid(ctx, m, sg, secs, body, an.Analysis)
	if err != nil {
		return resp, nil, err
	}
	if g == nil {
		resp.State = "sync"
		return resp, nil, nil
	}
	resp.SyncSource, resp.SyncEstimated = source, estimated

	// Each played bar's time signature: its section's, else the song's.
	global := melody.ParseMeter(orDefault(sg.Get("time"), "4/4"))
	meterOf := func(bar int) melody.Meter {
		for _, sec := range secs {
			if bar >= sec.StartBar && bar < sec.StartBar+sec.BarCount && sec.Index < len(sg.Sections) && sg.Sections[sec.Index].Time != "" {
				return melody.ParseMeter(sg.Sections[sec.Index].Time)
			}
		}
		return global
	}
	last := 0
	for _, sec := range secs {
		last = max(last, sec.StartBar+sec.BarCount)
	}
	sheet := melody.Quantize(m.Notes, g, meterOf, melody.Options{FirstBar: 0, LastBar: max(0, min(last, g.Bars())-1)})

	// The lyrics, line by line, each with its section's span.
	text := ""
	labels := sectionLabels(secs)
	var lines []melody.Line
	var shown []melodyLine
	for _, sec := range secs {
		for li, l := range sec.Lyrics {
			var b strings.Builder
			for _, seg := range l.Segments {
				b.WriteString(seg.Text)
			}
			t := strings.Join(strings.Fields(b.String()), " ")
			if t == "" {
				continue
			}
			text += t + "\n"
			from, to := math.NaN(), math.NaN()
			if sec.BarCount > 0 {
				from, to = g.TimeAt(float64(max(0, sec.StartBar-1))), g.TimeAt(float64(sec.StartBar+sec.BarCount+1))
			}
			key := fmt.Sprintf("%d:%d", sec.Index, li)
			lines = append(lines, melody.Line{Key: key, Section: sec.Index, Text: t, From: from, To: to})
			shown = append(shown, melodyLine{Key: key, Section: sec.Index, Line: li, Label: labels[sec.Index], Text: t, Bar: -1})
		}
	}
	lang := melody.GuessLang(text)
	resp.Lang = string(lang)
	for i := range lines {
		lines[i].Syls = melody.SplitLine(lines[i].Text, lang)
	}
	spans := make([]melody.Span, len(sheet.Notes))
	for i, p := range sheet.Notes {
		spans[i] = melody.Span{Start: m.Notes[p.Src].Start, End: m.Notes[p.Src].End}
	}
	var phrases []melody.Phrase
	if an.Analysis != nil && len(an.Analysis.VoicePhrases) > 0 {
		phrases = melody.PhrasesFromSpans(spans, an.Analysis.VoicePhrases)
	} else {
		phrases = melody.PhrasesFromNotes(spans, 0.35)
	}
	shifts := map[string]int{}
	if list, err := s.store.ListMelodyLyricShifts(ctx, variantID); err == nil {
		byKey := map[string]string{}
		for _, l := range lines {
			byKey[l.Key] = l.Text
		}
		for _, c := range list {
			k := fmt.Sprintf("%d:%d", c.Section, c.Line)
			if byKey[k] == c.Text {
				shifts[k] = c.Shift
			}
		}
	}
	var lyr []melody.NoteLyric
	low := map[int]bool{}
	if len(lines) > 0 {
		var fits []melody.LineFit
		fits, lyr = melody.AlignLines(lines, phrases, spans, shifts)
		var sum float64
		for i, f := range fits {
			shown[i].Matched, shown[i].Conf, shown[i].Shift = f.Matched, math.Round(f.Conf*100)/100, f.Shift
			if f.Matched {
				shown[i].Bar = sheet.Notes[f.First].Bar
			}
			if !f.Matched || f.Conf < melody.LowLineConf {
				low[i] = true
			}
			sum += f.Conf
		}
		resp.Confidence = math.Round(sum/float64(len(fits))*100) / 100
	}
	key, ok := melody.KeyOf(sg.Get("key"), transpose, sg.Get("flats") == "yes")
	if !ok && an.Analysis != nil {
		key, _ = melody.KeyOf(analysisKey(an.Analysis.Key), transpose, false)
	}
	tempo, _ := strconv.ParseFloat(sg.Get("tempo"), 64)
	sections := map[int]string{}
	for _, sec := range secs {
		if sec.BarCount > 0 {
			if _, ok := sections[sec.StartBar]; !ok {
				sections[sec.StartBar] = labels[sec.Index]
			}
		}
	}
	sc := &melody.Score{Sheet: sheet, Lyrics: lyr, LowLines: low, Transpose: transpose, Key: key, Tempo: tempo, Sections: sections, Title: title}
	resp.State = "ready"
	resp.AlphaTex, resp.Marks = sc.AlphaTex()
	for _, b := range sheet.Bars {
		resp.Bars = append(resp.Bars, b.Played)
	}
	resp.Lines = shown
	resp.Notes, resp.Dropped = len(sheet.Notes), sheet.Dropped
	for _, n := range sheet.Notes {
		if n.Low {
			resp.LowNotes++
		}
	}
	return resp, sc, nil
}

// analysisKey reads the analysis' key ("A:min", "C major", "F#m") as a
// chart's key: "" when it can't.
func analysisKey(k string) string {
	k = strings.TrimSpace(k)
	if root, mode, ok := strings.Cut(k, ":"); ok {
		if strings.HasPrefix(mode, "min") {
			return root + "m"
		}
		return root
	}
	if root, mode, ok := strings.Cut(k, " "); ok {
		if strings.HasPrefix(mode, "min") {
			return root + "m"
		}
		return root
	}
	return k
}

// sectionLabels numbers repeated labels as the Sync sheet does ("Verse 2").
func sectionLabels(secs []viewerSection) map[int]string {
	count, seen := map[string]int{}, map[string]int{}
	for _, s := range secs {
		count[s.Label]++
	}
	out := map[int]string{}
	for _, s := range secs {
		seen[s.Label]++
		l := s.Label
		if count[s.Label] > 1 && (l == "" || l[len(l)-1] < '0' || l[len(l)-1] > '9') {
			l = fmt.Sprintf("%s %d", l, seen[s.Label])
		}
		out[s.Index] = l
	}
	return out
}

// melodyGrid is the recording's bar grid as the song's player builds it
// (audio-player.js rebuildGrid): its per-bar points, else its sections'
// starts and the chart's tempo, else — nothing saved — the recording's
// own analysis laid on the chart (the estimate the Sync sheet offers),
// when it's sure enough; then the song's "lead:" placed. nil: no grid.
func (s *Server) melodyGrid(ctx context.Context, m *store.SongMelody, sg *song.Song, secs []viewerSection, body string, rec *chartalign.Recording) (melody.Grid, string, bool, error) {
	duration := 0.0
	if rec != nil {
		duration = rec.Duration
	}
	if n := len(m.Notes); n > 0 {
		duration = math.Max(duration, m.Notes[n-1].End+30)
	}
	tempo, _ := strconv.ParseFloat(orDefault(sg.Get("tempo"), "120"), 64)
	secondsPerBar := 0.0
	if tempo > 0 {
		secondsPerBar = 60 / tempo * float64(timeSigBeats(orDefault(sg.Get("time"), "4/4")))
	}
	points, err := s.store.ListGPBarSyncPoints(ctx, m.AudioID)
	if err != nil {
		return nil, "", false, err
	}
	var g melody.Grid
	source, estimated := "", false
	if len(points) > 1 {
		pts := make([]melody.Point, len(points))
		auto := true
		for i, p := range points {
			pts[i] = melody.Point{Bar: p.BarIndex, Time: p.TimeSeconds}
			auto = auto && p.Origin == store.SyncAuto
		}
		g, source, estimated = melody.FromPoints(pts, duration), "points", auto
	}
	if g == nil {
		markers, err := s.store.ListAudioMarkers(ctx, m.AudioID)
		if err != nil {
			return nil, "", false, err
		}
		start := map[int]int{}
		for _, sec := range secs {
			start[sec.Index] = sec.StartBar
		}
		var marks []melody.Mark
		auto := true
		for _, mk := range markers {
			if b, ok := start[mk.SectionIndex]; ok {
				marks = append(marks, melody.Mark{StartBar: b, Time: mk.TimeSeconds})
				auto = auto && mk.Origin == store.SyncAuto
			}
		}
		sortMarks(marks)
		if len(marks) > 0 {
			g, source, estimated = melody.FromMarkers(marks, secondsPerBar, duration), "markers", auto
		}
	}
	if g == nil && rec != nil {
		p := songimport.ProposeAutoSync(rec, body, SyncSections(sg), chartalign.Hints{})
		if p.State != "unsure" && len(p.BarPoints) > 1 {
			pts := make([]melody.Point, len(p.BarPoints))
			for i, bp := range p.BarPoints {
				pts[i] = melody.Point{Bar: bp.Bar, Time: bp.Time}
			}
			g, source, estimated = melody.FromPoints(pts, duration), "analysis", true
		}
	}
	if g == nil {
		return nil, "", false, nil
	}
	if lead := sg.LeadIn(); lead > 0 {
		count := 1
		for _, sec := range secs {
			if sec.BarCount > 0 {
				count = sec.BarCount
				break
			}
		}
		pace := melody.FirstPace(g, lead, count)
		if pace <= 0 {
			pace = secondsPerBar
		}
		g = melody.PlaceLead(g, lead, pace)
	}
	return g, source, estimated, nil
}

func sortMarks(m []melody.Mark) {
	for i := 1; i < len(m); i++ {
		for j := i; j > 0 && m[j].StartBar < m[j-1].StartBar; j-- {
			m[j], m[j-1] = m[j-1], m[j]
		}
	}
}

func melodyTranspose(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("transpose"))
	return max(-24, min(24, n))
}

// handleGetSongMelody is GET /api/songs/{id}/melody.
func (s *Server) handleGetSongMelody(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	resp, _, err := s.melodyScore(r.Context(), variantID, melodyTranspose(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetSongMelodyMusicXML is GET /api/songs/{id}/melody.musicxml.
func (s *Server) handleGetSongMelodyMusicXML(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	resp, sc, err := s.melodyScore(r.Context(), variantID, melodyTranspose(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sc == nil {
		httpError(w, http.StatusNotFound, "no melody yet ("+resp.State+")")
		return
	}
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < ' ' {
			return -1
		}
		return r
	}, sc.Title)
	if strings.TrimSpace(name) == "" {
		name = "melody"
	}
	w.Header().Set("Content-Type", "application/vnd.recordare.musicxml+xml")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + " - melody.musicxml"}))
	_, _ = w.Write(sc.MusicXML())
}

// handlePutMelodyLyricShift is PUT /api/songs/{id}/melody/lyrics-shift
// {section, line, shift}: where a line's syllables start, moved by so
// many notes (0 takes the correction back). The line's text is kept with
// it: the correction lapses when the line changes.
func (s *Server) handlePutMelodyLyricShift(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Section int `json:"section"`
		Line    int `json:"line"`
		Shift   int `json:"shift"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Section < 0 || req.Line < 0 || req.Shift < -64 || req.Shift > 64 {
		httpError(w, http.StatusBadRequest, "invalid correction")
		return
	}
	_, _, _, body, err := s.store.VariantContent(r.Context(), variantID)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	text := ""
	for _, sec := range buildSections(song.Parse(body)) {
		if sec.Index == req.Section && req.Line < len(sec.Lyrics) {
			var b strings.Builder
			for _, seg := range sec.Lyrics[req.Line].Segments {
				b.WriteString(seg.Text)
			}
			text = strings.Join(strings.Fields(b.String()), " ")
		}
	}
	if text == "" {
		httpError(w, http.StatusBadRequest, "no such lyric line")
		return
	}
	if err := s.store.SetMelodyLyricShift(r.Context(), variantID, store.MelodyLyricShift{Section: req.Section, Line: req.Line, Text: text, Shift: req.Shift}, actorID(r)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp, _, err := s.melodyScore(r.Context(), variantID, melodyTranspose(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
