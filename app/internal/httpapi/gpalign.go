package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"app/internal/gpfacts"
	"app/internal/song"
)

// A Guitar Pro file "next to the chart" (docs/gp-file-model.md §3.4): the
// song has no "gp[...]" lines, its chart is laid over the file bar for bar
// from "gp_start:" (the file's bar, as played, where the chart's first bar
// starts). alignChart compares the two section by section — bar counts
// against the file's own section markers, the chords over each bar, the
// time signature — for the GP import's check step (POST …/gp-check, before
// anything is saved) and for drift once the file is kept (computeDrift),
// so both say the same thing.

// alignMarker is one of the file's sections: a marker and the bars it runs
// for, until the next one. From is a played position, 1-based.
type alignMarker struct {
	Text string `json:"text"`
	From int    `json:"from"`
	Bars int    `json:"bars"`
	Bar  int    `json:"bar"` // its written bar, as Guitar Pro numbers it
	// Written are its written bars, as ranges, for drawing it.
	Written [][2]int `json:"written,omitempty"`
}

// alignSection is one section of the chart, in performed order, laid over
// the file.
type alignSection struct {
	Index int    `json:"index"` // into the song's sections
	Label string `json:"label"` // "Verse 2"
	Start int    `json:"start"` // performed bar, 0-based
	Bars  int    `json:"bars"`
	// FileFrom is the played position (1-based) its first bar falls on,
	// FileBars how many of its bars the file still has from there.
	FileFrom int `json:"fileFrom"`
	FileBars int `json:"fileBars"`
	// Written are the file's written bars it falls on, as ranges, for
	// drawing them (Guitar Pro's numbers).
	Written [][2]int `json:"written,omitempty"`
	// Match is the file's section paired with it (by position, then
	// name, then order); Shift how many bars later (> 0) or earlier the
	// file's section starts than this one falls; Own when that shift
	// isn't explained by an earlier section's bar count.
	Match *alignMarker `json:"match,omitempty"`
	Shift int          `json:"shift"`
	Own   bool         `json:"own"`
	// The chords over each bar, the chart's and the file's, as they sound
	// (a chord carried over a bar repeated), and the bars (0-based) where
	// they differ.
	Chart     []string `json:"chart"`
	File      []string `json:"file"`
	ChordDiff []int    `json:"chordDiff,omitempty"`
	ChartTime string   `json:"chartTime"`
	FileTime  string   `json:"fileTime,omitempty"`
	// Status: "ok", "missing" (the file ends before it), "bars" (the
	// file's section is longer or shorter), "shifted", "chords", "time".
	Status string `json:"status"`
}

type gpAlignment struct {
	Start       int            `json:"start"`
	ChartBars   int            `json:"chartBars"`
	FileBars    int            `json:"fileBars"` // as played
	ChartPickup bool           `json:"chartPickup"`
	FilePickup  bool           `json:"filePickup"`
	FileEmpty   int            `json:"fileEmpty"`
	HasChords   bool           `json:"hasChords"`
	Markers     []alignMarker  `json:"markers"`
	Sections    []alignSection `json:"sections"`
	// BestStart is where the chords line up best, when that's elsewhere
	// than Start and lines up more bars (0 otherwise).
	BestStart int  `json:"bestStart,omitempty"`
	Aligned   bool `json:"aligned"`
}

// gpStart is the song's "gp_start:", 1 when absent or unreadable.
func gpStart(sg *song.Song) int {
	n, err := strconv.Atoi(strings.TrimSpace(sg.Get("gp_start")))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// anyGPLine reports whether any section points into the file.
func anyGPLine(sg *song.Song) bool {
	for _, sec := range sg.Sections {
		if len(sec.GP) > 0 {
			return true
		}
	}
	return false
}

// fileMarkers lists the file's sections in played order. A section played
// again through a repeat ("|: Chorus :|") is one section, its passes
// included, as Guitar Pro shows its marker once.
func fileMarkers(f *gpfacts.Facts) []alignMarker {
	var out []alignMarker
	for p, w := range f.Played {
		if m := f.MarkerAt(w); m != "" {
			if n := len(out); n > 0 && out[n-1].Bar == w {
				continue
			}
			if n := len(out); n > 0 {
				out[n-1].Bars = p + 1 - out[n-1].From
			}
			out = append(out, alignMarker{Text: m, From: p + 1, Bar: w})
		}
	}
	if n := len(out); n > 0 {
		out[n-1].Bars = len(f.Played) + 1 - out[n-1].From
	}
	for i := range out {
		out[i].Written = writtenRanges(f, out[i].From, out[i].Bars)
	}
	return out
}

func sameSectionName(marker, label string, number int) bool {
	m := gpfacts.Fold(marker)
	l := gpfacts.Fold(label)
	if number > 0 {
		if m == l+" "+strconv.Itoa(number) {
			return true
		}
	}
	return m == l || (l != "" && strings.HasPrefix(m, l))
}

// chartChords is a section's chords per performed bar as they sound, and
// the chord in effect after it.
func fileChordsAt(f *gpfacts.Facts, pos int, prev string) ([]string, string) {
	var fc []gpfacts.BarChord
	if pos >= 1 && pos <= len(f.Played) {
		if w := f.Played[pos-1]; w-1 < len(f.Chords) {
			fc = f.Chords[w-1]
		}
	}
	return effectiveChords(barChordsJSON(fc), prev, gpfacts.ChordName)
}

// writtenRanges folds played positions [from, from+n) into the file's
// written bar ranges, in order of first appearance.
func writtenRanges(f *gpfacts.Facts, from, n int) [][2]int {
	var out [][2]int
	seen := map[int]bool{}
	for p := from; p < from+n && p <= len(f.Played); p++ {
		if p < 1 {
			continue
		}
		w := f.Played[p-1]
		if seen[w] {
			continue
		}
		seen[w] = true
		if k := len(out); k > 0 && out[k-1][1] == w-1 {
			out[k-1][1] = w
		} else {
			out = append(out, [2]int{w, w})
		}
	}
	return out
}

// alignChart lays the chart's sections over the file from start.
func alignChart(sg *song.Song, sections []viewerSection, f *gpfacts.Facts, start int) gpAlignment {
	if start < 1 {
		start = 1
	}
	al := gpAlignment{Start: start, FileBars: len(f.Played), FilePickup: f.Pickup, FileEmpty: f.Empty, Markers: fileMarkers(f), Sections: []alignSection{}}
	for _, c := range f.Chords {
		if len(c) > 0 {
			al.HasChords = true
			break
		}
	}
	if len(sections) > 0 && len(sections[0].Measures) > 0 {
		al.ChartPickup = sections[0].Measures[0].Pickup
	}
	if al.Markers == nil {
		al.Markers = []alignMarker{}
	}
	songTime := orDefault(strings.TrimSpace(sg.Get("time")), "4/4")

	cursor := 0 // the next file section to pair
	var tPrev, fPrev string
	expected := 0 // the shift the sections so far account for
	for i := range sections {
		sec := &sections[i]
		if sec.BarCount <= 0 {
			continue
		}
		if end := sec.StartBar + sec.BarCount; end > al.ChartBars {
			al.ChartBars = end
		}
		row := alignSection{Index: sec.Index, Label: sectionName(sec), Start: sec.StartBar, Bars: sec.BarCount, Status: "ok"}
		from := start + sec.StartBar
		if from <= len(f.Played) {
			row.FileFrom = from
			row.FileBars = min(sec.BarCount, len(f.Played)-from+1)
			row.Written = writtenRanges(f, from, row.FileBars)
		}
		row.ChartTime = songTime
		if sec.Index >= 0 && sec.Index < len(sg.Sections) && sg.Sections[sec.Index].Time != "" {
			row.ChartTime = sg.Sections[sec.Index].Time
		}
		if row.FileFrom > 0 {
			num, den := f.TimeAt(f.Played[row.FileFrom-1])
			row.FileTime = fmt.Sprintf("%d/%d", num, den)
		}

		// The file's section for it: one starting where it falls, else
		// one of the next few with its name, else the next one.
		if cursor < len(al.Markers) {
			pick := -1
			for k := cursor; k < len(al.Markers) && k < cursor+3; k++ {
				if al.Markers[k].From == from {
					pick = k
					break
				}
			}
			if pick < 0 {
				for k := cursor; k < len(al.Markers) && k < cursor+3; k++ {
					if sameSectionName(al.Markers[k].Text, sec.Label, sec.LabelNumber) {
						pick = k
						break
					}
				}
			}
			if pick < 0 {
				pick = cursor
			}
			m := al.Markers[pick]
			row.Match = &m
			row.Shift = m.From - from
			cursor = pick + 1
		}

		// Chords, bar by bar, under this start.
		text := performedChordsAs(sections, i, func(s string) string { return strings.TrimSpace(s) })
		for b := 0; b < sec.BarCount; b++ {
			var tSeq, fSeq []string
			var bar []mixerChordJSON
			if b < len(text) {
				bar = text[b]
			}
			tSeq, tPrev = effectiveChords(bar, tPrev, nil)
			fSeq, fPrev = fileChordsAt(f, from+b, fPrev)
			row.Chart = append(row.Chart, strings.Join(tSeq, " "))
			row.File = append(row.File, strings.Join(fSeq, " "))
			if al.HasChords && from+b <= len(f.Played) && !sameChordSeq(tSeq, fSeq) {
				row.ChordDiff = append(row.ChordDiff, b)
			}
		}

		switch {
		case row.FileFrom == 0 || row.FileBars < row.Bars:
			row.Status = "missing"
		case row.Match != nil && row.Match.Bars != row.Bars:
			row.Status = "bars"
		case row.Match != nil && row.Shift != 0:
			row.Status = "shifted"
		case len(row.ChordDiff) > 0:
			row.Status = "chords"
		case row.FileTime != "" && row.FileTime != row.ChartTime:
			row.Status = "time"
		}
		if row.Match != nil {
			// A shift is this section's own unless the sections before ran
			// longer or shorter in the file by just as much.
			row.Own = row.Shift != expected
			expected = row.Shift + row.Match.Bars - row.Bars
		}
		al.Sections = append(al.Sections, row)
	}

	al.Aligned = len(al.Sections) > 0
	for _, r := range al.Sections {
		if r.Status != "ok" {
			al.Aligned = false
		}
	}
	if al.HasChords && al.ChartBars > 0 {
		cur := chordsLinedUp(sections, f, start)
		best, bestN := start, cur
		for s := 1; s <= 17 && s <= len(f.Played); s++ {
			if n := chordsLinedUp(sections, f, s); n > bestN {
				best, bestN = s, n
			}
		}
		if best != start {
			al.BestStart = best
		}
	}
	return al
}

// chordsLinedUp counts the chart's bars that name a chord and sound the
// same chords as the file's bar they fall on, from start.
func chordsLinedUp(sections []viewerSection, f *gpfacts.Facts, start int) int {
	n := 0
	var tPrev, fPrev string
	for i := range sections {
		sec := &sections[i]
		text := performedChordsAs(sections, i, func(s string) string { return strings.TrimSpace(s) })
		for b := 0; b < sec.BarCount; b++ {
			var bar []mixerChordJSON
			if b < len(text) {
				bar = text[b]
			}
			var tSeq, fSeq []string
			tSeq, tPrev = effectiveChords(bar, tPrev, nil)
			fSeq, fPrev = fileChordsAt(f, start+sec.StartBar+b, fPrev)
			if len(bar) > 0 && len(tSeq) > 0 && sameChordSeq(tSeq, fSeq) {
				n++
			}
		}
	}
	return n
}

// playedBarsOf is the file's played bars (written numbers) a chart section
// falls on, from the song's "gp_start:".
func playedBarsOf(f *gpfacts.Facts, start, startBar, count int) []int {
	var out []int
	for p := start + startBar; p < start+startBar+count && p <= len(f.Played); p++ {
		if p >= 1 {
			out = append(out, f.Played[p-1])
		}
	}
	return out
}

// alignDrift is drift for a song next to its file (no "gp[...]" lines):
// the same differences the GP import's check step shows.
func alignDrift(sg *song.Song, sections []viewerSection, f *gpfacts.Facts, add func(driftItem)) {
	if anyGPLine(sg) || anyScoreVoice(sg) || len(f.Played) == 0 {
		return
	}
	alignItems(sg, sections, f, gpStart(sg), add)
}

// alignItems are the differences of the chart laid over the file from
// start, as drift items: what alignDrift reports, and what the check step
// counts before the import.
func alignItems(sg *song.Song, sections []viewerSection, f *gpfacts.Facts, start int, add func(driftItem)) {
	al := alignChart(sg, sections, f, start)
	if len(al.Sections) == 0 {
		return
	}
	// Where the chart starts: the first section paired with a file
	// section that starts elsewhere.
	if first := al.Sections[0]; first.Match != nil && first.Shift != 0 && start+first.Shift >= 1 {
		add(driftItem{Fact: "start", Title: "Where the chart starts in the file", Where: first.Label,
			Text: "bar " + strconv.Itoa(start), File: fmt.Sprintf("bar %d (%q)", start+first.Shift, first.Match.Text), Take: true, Bar: first.Match.Bar})
	}
	headerTime := orDefault(strings.TrimSpace(sg.Get("time")), "4/4")
	for k, r := range al.Sections {
		switch {
		case r.Status == "missing":
			add(driftItem{Fact: fmt.Sprintf("bars:%d", r.Index), Title: "Bar count", Where: r.Label,
				Text: countOf(r.Bars, "bar"), File: countOf(max(r.FileBars, 0), "bar") + " (the file ends)"})
		case r.Status == "bars":
			add(driftItem{Fact: fmt.Sprintf("bars:%d", r.Index), Title: "Bar count", Where: r.Label,
				Text: countOf(r.Bars, "bar"), File: fmt.Sprintf("%s (%q, bar %d)", countOf(r.Match.Bars, "bar"), r.Match.Text, r.Match.Bar), Bar: r.Match.Bar})
		case r.Status == "shifted" && r.Own && k > 0:
			add(driftItem{Fact: fmt.Sprintf("shift:%d", r.Index), Title: "Section start", Where: r.Label,
				Text: "bar " + strconv.Itoa(r.FileFrom), File: fmt.Sprintf("bar %d (%q)", r.Match.From, r.Match.Text), Bar: r.Match.Bar})
		case r.Status == "chords":
			var bars, tShow, fShow []string
			for j, b := range r.ChordDiff {
				bars = append(bars, strconv.Itoa(b+1))
				if j < 4 {
					tShow = append(tShow, r.Chart[b])
					fShow = append(fShow, r.File[b])
				}
			}
			what := "bar " + bars[0]
			if len(bars) > 1 {
				what = "bars " + strings.Join(bars, ", ")
			}
			bar := 0
			if r.FileFrom > 0 && r.FileFrom+r.ChordDiff[0] <= len(f.Played) {
				bar = f.Played[r.FileFrom+r.ChordDiff[0]-1]
			}
			add(driftItem{Fact: fmt.Sprintf("chords:%d", r.Index), Title: "Chords, " + what, Where: r.Label,
				Text: strings.Join(tShow, " | "), File: strings.Join(fShow, " | "), Take: true, Bar: bar})
		case r.Status == "time":
			// The header's own difference is reported once, as "time".
			if k == 0 || (r.ChartTime == headerTime && len(f.Time) > 0 && r.FileTime == fmt.Sprintf("%d/%d", f.Time[0].Num, f.Time[0].Den)) {
				continue
			}
			add(driftItem{Fact: fmt.Sprintf("time:%d", r.Index), Title: "Time signature", Where: r.Label, Text: r.ChartTime, File: r.FileTime})
		}
	}
}

// handlePostGPCheck is POST /api/songs/{id}/gp-check {facts, start}: the GP
// import's check step, before the file is kept — the header facts that
// differ (as drift will say them) and the chart laid over the file from
// start (alignChart).
func (s *Server) handlePostGPCheck(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Facts *gpfacts.Facts `json:"facts"`
		Start int            `json:"start"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Facts == nil {
		httpError(w, http.StatusBadRequest, "facts expected")
		return
	}
	sg, _, err := s.store.LoadVariant(id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	start := req.Start
	if start < 1 {
		start = gpStart(sg)
	}
	sections := buildSections(sg)
	var header []driftItem
	for _, it := range computeDrift(sg, sections, req.Facts, nil) {
		switch it.Fact {
		case "tempo", "time", "key", "swing":
			header = append(header, it)
		}
	}
	// The differences drift will show for the sections once the file is
	// kept with this start: what's left, as the check step counts it.
	var items []driftItem
	alignItems(sg, sections, req.Facts, start, func(it driftItem) { items = append(items, it) })
	writeJSON(w, http.StatusOK, map[string]any{
		"header":    nonNilDrift(header),
		"items":     nonNilDrift(items),
		"alignment": alignChart(sg, sections, req.Facts, start),
	})
}
