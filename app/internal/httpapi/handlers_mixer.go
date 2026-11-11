package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"app/internal/chordsym"

	"app/internal/tab"
)

type mixerChordJSON struct {
	Pos  float64 `json:"pos"` // fraction of the bar, 0 = downbeat
	Name string  `json:"name"`
}

type mixerSectionJSON struct {
	Index    int                `json:"index"`
	Label    string             `json:"label"`
	StartBar int                `json:"startBar"`
	BarCount int                `json:"barCount"`
	Chords   [][]mixerChordJSON `json:"chords"` // one entry per performed bar
}

type mixerTrackJSON struct {
	Name    string `json:"name"`
	Drums   bool   `json:"drums"`
	Program int    `json:"program"`
}

// handleGetSongMixer is GET /api/songs/{id}/mixer — the Mixer view's whole
// timeline: every section in performed order with its chords per bar, and
// the whole song's score as ONE multi-track alphaTeX document whose bar i
// is performed bar i (the same index the GP bar sync points and the
// structure strip use), so the client can lay sections, chords, the
// reference recording and every score track on one shared time axis.
func (s *Server) handleGetSongMixer(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	sg, _, err := s.store.LoadVariant(variantID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sections := buildSections(sg)
	num, den := timeSigParts(orDefault(sg.Get("time"), "4/4"))
	timeNum, timeDen := atoiOr(num, 4), atoiOr(den, 4)
	tempo := atoiOr(orDefault(sg.Get("tempo"), "120"), 120)

	outSections := make([]mixerSectionJSON, 0, len(sections))
	totalBars := 0
	for i := range sections {
		sec := &sections[i]
		if sec.BarCount <= 0 {
			continue
		}
		label := labelUnescaper.Replace(sec.Label)
		if sec.LabelNumber != 0 {
			label += " " + strconv.Itoa(sec.LabelNumber)
		}
		outSections = append(outSections, mixerSectionJSON{
			Index: sec.Index, Label: label, StartBar: sec.StartBar, BarCount: sec.BarCount,
			Chords: performedChords(sections, i),
		})
		if end := sec.StartBar + sec.BarCount; end > totalBars {
			totalBars = end
		}
	}

	riffs, _ := tab.LoadScores(sg)
	tracks := wholeSongTracks(sections, riffs, timeNum, timeDen)
	pickup := pickupBeats(tracks, timeNum, timeDen)
	resp := map[string]any{
		"tempo": tempo, "timeNum": timeNum, "timeDen": timeDen, "key": sg.Get("key"),
		"totalBars": totalBars, "sections": outSections, "tracks": []mixerTrackJSON{},
		"pickupBeats": pickup,
	}
	if len(tracks) > 0 {
		tex, meta := tab.ToAlphaTexMulti(tracks, tempo, timeNum, timeDen, pickup > 0)
		out := make([]mixerTrackJSON, len(meta))
		for i, m := range meta {
			out[i] = mixerTrackJSON{Name: m.Name, Drums: m.Drums, Program: m.Program}
		}
		resp["alphaTex"] = tex
		resp["tracks"] = out
	}
	writeJSON(w, http.StatusOK, resp)
}

// definingSection is the section a bare reference section (IsReference —
// "Chorus" with no grid of its own, repeating the last real "Chorus")
// borrows its content from, or -1.
func definingSection(sections []viewerSection, i int) int {
	if !sections[i].IsReference {
		return -1
	}
	label := strings.TrimSpace(sections[i].Label)
	for j := i - 1; j >= 0; j-- {
		if strings.EqualFold(strings.TrimSpace(sections[j].Label), label) && len(sections[j].Measures) > 0 {
			return j
		}
	}
	return -1
}

// writtenChords is a section's chords per WRITTEN bar, with "%"/"%%"
// simile bars resolved to the bar(s) they repeat.
func writtenChords(sec *viewerSection) [][]mixerChordJSON {
	var out [][]mixerChordJSON
	for _, m := range sec.Measures {
		if m.IsRepeat {
			n := m.RepeatCount
			if n < 1 {
				n = 1
			}
			var prev []mixerChordJSON
			if len(out) >= n {
				prev = out[len(out)-n]
			}
			out = append(out, prev)
			continue
		}
		var bar []mixerChordJSON
		if !m.IsRest && len(m.Beats) > 0 {
			for bi, beat := range m.Beats {
				if beat.IsTie || strings.TrimSpace(beat.Text) == "" {
					continue
				}
				bar = append(bar, mixerChordJSON{Pos: float64(bi) / float64(len(m.Beats)), Name: chordsym.Glyphs(beat.Text)})
			}
		}
		out = append(out, bar)
	}
	return out
}

// performedChords expands a section's written chord bars into its
// PERFORMED bar sequence (BarCount long): "|: ... :|" brackets replayed
// their stated count (2 when unmarked), then the whole result cycled to
// fill BarCount — which covers a "times: N" section and a bare reference
// borrowing an earlier section's grid. Volta endings are not modeled.
func performedChords(sections []viewerSection, i int) [][]mixerChordJSON {
	sec := &sections[i]
	src := sec
	if len(sec.Measures) == 0 {
		if j := definingSection(sections, i); j >= 0 {
			src = &sections[j]
		}
	}
	written := writtenChords(src)
	var expanded [][]mixerChordJSON
	for k := 0; k < len(src.Measures); k++ {
		if src.Measures[k].Open {
			end := k
			for end < len(src.Measures) && !src.Measures[end].Close {
				end++
			}
			if end < len(src.Measures) {
				count := src.Measures[end].BracketRepeatCount
				if count < 2 {
					count = 2
				}
				for p := 0; p < count; p++ {
					expanded = append(expanded, written[k:end+1]...)
				}
				k = end
				continue
			}
		}
		expanded = append(expanded, written[k])
	}
	out := make([][]mixerChordJSON, sec.BarCount)
	if len(expanded) == 0 {
		return out
	}
	for b := range out {
		out[b] = expanded[b%len(expanded)]
	}
	return out
}

// restBar is one full bar of rests in num/den time.
func restBar(num, den int) tab.Bar {
	if num == 4 && den == 4 {
		return tab.Bar{Beats: []tab.Beat{{Rest: true, Duration: "1"}}}
	}
	bar := tab.Bar{}
	for i := 0; i < num; i++ {
		bar.Beats = append(bar.Beats, tab.Beat{Rest: true, Duration: strconv.Itoa(den)})
	}
	return bar
}

// wholeSongTracks stitches every section's own named riffs into one
// whole-song riff per track, in performed order: each section contributes
// exactly BarCount bars — its riff padded with rest bars (same convention
// as chipSource/Drummer view) or truncated, a bare reference section
// borrows its defining section's riff, and a section with no riff for
// that track is all rests.
func wholeSongTracks(sections []viewerSection, riffs map[int][]tab.NamedRiff, num, den int) []tab.NamedRiff {
	type key struct {
		name  string
		drums bool
	}
	var order []key
	first := map[key]*tab.Riff{}
	for i := range sections {
		for _, nr := range riffs[sections[i].Index] {
			k := key{nr.Name, nr.Riff.Drums}
			if _, ok := first[k]; !ok {
				first[k] = nr.Riff
				order = append(order, k)
			}
		}
	}
	find := func(si int, k key) *tab.Riff {
		for _, nr := range riffs[sections[si].Index] {
			if nr.Name == k.name && nr.Riff.Drums == k.drums {
				return nr.Riff
			}
		}
		return nil
	}
	out := make([]tab.NamedRiff, 0, len(order))
	for _, k := range order {
		var bars []tab.Bar
		for i := range sections {
			n := sections[i].BarCount
			if n <= 0 {
				continue
			}
			r := find(i, k)
			if r == nil {
				if j := definingSection(sections, i); j >= 0 {
					r = find(j, k)
				}
			}
			var sb []tab.Bar
			if r != nil {
				sb = r.Bars
			}
			if len(sb) > n {
				sb = sb[:n]
			}
			bars = append(bars, sb...)
			for p := len(sb); p < n; p++ {
				bars = append(bars, restBar(num, den))
			}
		}
		f := first[k]
		out = append(out, tab.NamedRiff{Name: k.name, Riff: &tab.Riff{Tuning: f.Tuning, MinimumFret: f.MinimumFret, Drums: k.drums, Bars: bars}})
	}
	return out
}

// labelUnescaper mirrors the "label" template func (webassets.unescapeTypst):
// section labels are stored Typst-escaped.
var labelUnescaper = strings.NewReplacer(`\&`, "&", `\%`, "%", `\#`, "#", `\_`, "_", `\*`, "*")

// pickupBeats detects a pickup (anacrusis) bar: when every track whose
// first bar has notes writes it shorter than a full bar — a Guitar Pro
// import keeps the file's own short first bar — the song starts with a
// pickup of that many quarter-note beats. Tracks with only rests there
// get a rest of the same length, so all tracks agree. 0 means none.
func pickupBeats(tracks []tab.NamedRiff, num, den int) float64 {
	full := float64(num) * 4 / float64(den)
	pickup := 0.0
	for _, t := range tracks {
		if len(t.Riff.Bars) == 0 || isRestBar(t.Riff.Bars[0]) {
			continue
		}
		b := tab.BarBeats(t.Riff.Bars[0])
		if b >= full-1e-9 {
			return 0
		}
		if b > pickup {
			pickup = b
		}
	}
	if pickup == 0 {
		return 0
	}
	for _, t := range tracks {
		if len(t.Riff.Bars) > 0 && isRestBar(t.Riff.Bars[0]) {
			t.Riff.Bars[0] = tab.Bar{Beats: tab.RestBeats(pickup)}
		}
	}
	return pickup
}

func isRestBar(b tab.Bar) bool {
	for _, beat := range b.Beats {
		if !beat.Rest {
			return false
		}
	}
	return true
}
