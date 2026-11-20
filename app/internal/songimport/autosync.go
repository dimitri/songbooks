package songimport

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"app/internal/chartalign"
	"app/internal/song"
)

// AutoSync is what the recording's own analysis (its beats, bars and
// chords, found by the stems worker: chartalign) says about a chart:
// where its bars fall, and what in it the recording contradicts. Every
// change is a proposal: Apply makes the ones an editor picked.
type AutoSync struct {
	*chartalign.Result
	// Labels as the Sync sheet numbers them ("Verse 2").
	Labels map[int]string `json:"labels"`
	// The chart's header and what the recording says it should be ("" or
	// 0: nothing to change).
	ChartTempo   string `json:"chartTempo"`
	ChartTimeSig string `json:"chartTimeSig"`
	ProposeTempo int    `json:"proposeTempo,omitempty"`
	ProposeTime  string `json:"proposeTime,omitempty"`
	// Markers: each found section's start, for the Sync sheet.
	Markers []Marker `json:"markers"`
	Notes   []string `json:"notes,omitempty"`
	notes   []note
}

// Marker is a section's start.
type Marker struct {
	SectionIndex int     `json:"sectionIndex"`
	TimeSeconds  float64 `json:"timeSeconds"`
}

func (a *AutoSync) note(msg string, args ...any) {
	a.notes = append(a.notes, note{msg, args})
	a.Notes = append(a.Notes, fmt.Sprintf(msg, args...))
}

// Localize says the notes again in the page's language.
func (a *AutoSync) Localize(tr func(msg string, args ...any) string) {
	a.Notes = a.Notes[:0]
	for _, n := range a.notes {
		a.Notes = append(a.Notes, tr(n.msg, n.args...))
	}
}

// ChartSections reads body's sections as chartalign plays them, numbered
// as the player numbers them (sections: httpapi.SyncSections of body).
func ChartSections(body string, sections []SyncSection) (*song.Song, []chartalign.Section) {
	sg := song.Parse(body)
	info := make([]chartalign.SectionInfo, len(sections))
	for i, s := range sections {
		info[i] = chartalign.SectionInfo{Index: s.Index, Label: s.Label, StartBar: s.StartBar, BarCount: s.BarCount, HasLyrics: s.HasLyrics}
	}
	return sg, chartalign.Chart(sg, info)
}

// ProposeAutoSync aligns body to the recording's analysis, with an
// editor's hints about its beat grid.
func ProposeAutoSync(rec *chartalign.Recording, body string, sections []SyncSection, h chartalign.Hints) *AutoSync {
	sg, secs := ChartSections(body, sections)
	beats := chartalign.BeatsOf(orDefault(sg.Get("time"), "4/4"))
	res := chartalign.Align(rec, secs, beats, h)
	a := &AutoSync{Result: res, Labels: map[int]string{}, ChartTempo: sg.Get("tempo"), ChartTimeSig: sg.Get("time"), Markers: []Marker{}}
	count, seen := map[string]int{}, map[string]int{}
	for _, s := range sections {
		count[s.Label]++
	}
	for _, s := range sections {
		seen[s.Label]++
		l := s.Label
		if count[s.Label] > 1 && !regexp.MustCompile(`\d\s*$`).MatchString(l) {
			l = fmt.Sprintf("%s %d", l, seen[s.Label])
		}
		a.Labels[s.Index] = l
	}
	for _, s := range res.Sections {
		if s.Start >= 0 {
			a.Markers = append(a.Markers, Marker{s.Index, s.Start})
		}
	}
	// Only what the recording is sure of is proposed for the chart: a
	// section's grid where its chords fit (0.5 or more), nothing when the
	// beat itself is unclear.
	for i := range res.Sections {
		if res.Sections[i].Confidence < 0.5 || res.State == "unsure" {
			res.Sections[i].Grid = ""
		}
	}
	if res.State == "unsure" {
		res.Intro, res.ChordFixes = nil, []chartalign.ChordFix{}
		a.note("the recording's beat is unclear (rubato, live, no drums?): nothing is proposed for the tempo, place the sections by ear")
		return a
	}
	// The tempo, when it's steady, in the chart's beat.
	if res.TempoSteady && res.Tempo > 0 {
		t := res.Tempo
		if chartBeats := chartalign.BeatsOf(orDefault(sg.Get("time"), res.TimeSig)); chartBeats != res.BeatsPerBar && res.BeatsPerBar > 0 {
			t = t * float64(chartBeats) / float64(res.BeatsPerBar)
		}
		want := int(math.Round(t))
		cur, _ := strconv.ParseFloat(sg.Get("tempo"), 64)
		if cur <= 0 || math.Abs(cur/float64(want)-1) > 0.03 {
			a.ProposeTempo = want
		}
	} else if res.Tempo > 0 {
		a.note("the tempo moves (about %d to %d bpm): no single tempo is proposed; the sync follows every bar", int(math.Round(res.TempoMin)), int(math.Round(res.TempoMax)))
	}
	// The metre, when the chart has none, or when the recording's bar
	// isn't one the chart's can be written as: 2/4 against 4/4 (a
	// downbeat every half bar), 12/8 against 4/4 (a shuffle) are the same
	// music written two ways, and the chart's way stays.
	if chart := sg.Get("time"); res.TimeSigConf >= 0.8 && res.TimeSig != "" && chart != res.TimeSig {
		cb, rb := chartalign.BeatsOf(chart), chartalign.BeatsOf(res.TimeSig)
		if chart == "" || (cb%rb != 0 && rb%cb != 0) {
			a.ProposeTime = res.TimeSig
		}
	}
	if res.Transpose != 0 {
		a.note("the chart's chords fit the recording %d semitones higher: is it written for a capo, or is the recording in another key?", res.Transpose)
	}
	return a
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// AutoFix is the changes an editor picked among an AutoSync's proposals.
type AutoFix struct {
	Tempo bool `json:"tempo"`
	Time  bool `json:"time"`
	Intro bool `json:"intro"`
	// Sections (by index) whose grid becomes the recording's bars.
	Sections []int `json:"sections"`
	// Chord fixes, as "defSection:measure:slot".
	Chords []string `json:"chords"`
}

// FixKey names a chord fix.
func FixKey(f chartalign.ChordFix) string {
	return fmt.Sprintf("%d:%d:%d", f.DefSection, f.Measure, f.Slot)
}

// Change is one line of the chart an AutoFix changes, for the editor to
// review before it's saved (headers and grids only: lyrics never move).
type Change struct {
	Where  string `json:"where"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// ApplyAutoFix makes the picked changes to body: chord fixes first (in
// their measures), then the picked sections regrouped on the recording's
// bars (aligned again with those chords), the intro, the headers. sync
// gives the sections of a body (httpapi.SyncSections).
func ApplyAutoFix(rec *chartalign.Recording, body string, sync func(string) []SyncSection, h chartalign.Hints, fix AutoFix) (string, []Change) {
	out := body
	if len(fix.Chords) > 0 {
		p := ProposeAutoSync(rec, out, sync(out), h)
		want := map[string]bool{}
		for _, k := range fix.Chords {
			want[k] = true
		}
		for _, f := range p.ChordFixes {
			if want[FixKey(f)] {
				out = replaceToken(out, f.DefSection, f.Measure, f.Slot, f.Heard)
			}
		}
	}
	if len(fix.Sections) > 0 || fix.Intro {
		p := ProposeAutoSync(rec, out, sync(out), h)
		picked := map[int]bool{}
		for _, i := range fix.Sections {
			picked[i] = true
		}
		for _, s := range p.Sections {
			if picked[s.Index] && s.Grid != "" {
				out = replaceGrid(out, s.Index, s.Grid)
			}
		}
		if fix.Intro && p.Intro != nil {
			out = insertIntro(out, p.Intro.Grid)
		}
	}
	if fix.Tempo || fix.Time {
		p := ProposeAutoSync(rec, out, sync(out), h)
		if fix.Time && p.ProposeTime != "" {
			out = SetHeader(out, "time", p.ProposeTime, true)
		}
		if fix.Tempo && p.ProposeTempo > 0 {
			out = SetHeader(out, "tempo", strconv.Itoa(p.ProposeTempo), true)
		}
	}
	return out, Changes(body, out)
}

var sectionHeaderRE = regexp.MustCompile(`^\[([^\[\]]+)\](\s*\(repeat\))?$`)

// sectionSpans finds each section's lines: [start, end) of every
// section, start being its header line.
func sectionSpans(lines []string) [][2]int {
	var out [][2]int
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			continue
		}
		if sectionHeaderRE.MatchString(t) {
			if n := len(out); n > 0 {
				out[n-1][1] = i
			}
			out = append(out, [2]int{i, len(lines)})
		}
	}
	return out
}

func isKey(line, key string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, key+":")
}

// replaceGrid gives section idx one grid line, grid, in place of its own
// (and of its "times:", folded into the grid).
func replaceGrid(body string, idx int, grid string) string {
	lines := strings.Split(body, "\n")
	secs := sectionSpans(lines)
	if idx < 0 || idx >= len(secs) {
		return body
	}
	a, b := secs[idx][0], secs[idx][1]
	var out []string
	out = append(out, lines[:a+1]...)
	placed := false
	for _, l := range lines[a+1 : b] {
		if isKey(l, "grid") {
			if !placed {
				out = append(out, "grid: "+grid)
				placed = true
			}
			continue
		}
		if isKey(l, "times") {
			continue
		}
		out = append(out, l)
	}
	if !placed {
		// A bare section gets a grid of its own, right under its header.
		rest := append([]string{"grid: " + grid}, out[a+1:]...)
		out = append(out[:a+1], rest...)
	}
	out = append(out, lines[b:]...)
	return strings.Join(out, "\n")
}

// insertIntro adds an [Intro] before the first section.
func insertIntro(body, grid string) string {
	lines := strings.Split(body, "\n")
	secs := sectionSpans(lines)
	at := len(lines)
	if len(secs) > 0 {
		at = secs[0][0]
	}
	add := []string{"[Intro]", "grid: " + grid, ""}
	out := append(append(append([]string{}, lines[:at]...), add...), lines[at:]...)
	return strings.Join(out, "\n")
}

// replaceToken changes one chord in a measure of section def: the slot-th
// token of its measure-th measure (counted over all its grid lines, as
// the parser counts them), keeping a ":duration" tag.
func replaceToken(body string, def, measure, slot int, chord string) string {
	lines := strings.Split(body, "\n")
	secs := sectionSpans(lines)
	if def < 0 || def >= len(secs) {
		return body
	}
	k := 0
	for li := secs[def][0] + 1; li < secs[def][1]; li++ {
		if !isKey(lines[li], "grid") {
			continue
		}
		line := lines[li]
		colon := strings.Index(line, "grid:")
		spec := line[colon+len("grid:"):]
		chunks := strings.Split(spec, "|")
		for ci, ch := range chunks {
			if !isMeasure(ch) {
				continue
			}
			if k == measure {
				chunks[ci] = setToken(ch, slot, chord)
				lines[li] = line[:colon+len("grid:")] + strings.Join(chunks, "|")
				return strings.Join(lines, "\n")
			}
			k++
		}
	}
	return body
}

// isMeasure mirrors song's parseGrid: a chunk between bar lines is a
// measure unless it's empty (a repeat sign alone still is).
func isMeasure(ch string) bool {
	t := strings.TrimSpace(ch)
	return t != ""
}

var chunkPrefixRE = regexp.MustCompile(`^(\s*:\d*\s*)?(\{[^}]*\}\s*)?(\([^)]*\)\s*)?`)

func setToken(ch string, slot int, chord string) string {
	pre := chunkPrefixRE.FindString(ch)
	rest := ch[len(pre):]
	suffix := ""
	trimmed := strings.TrimRight(rest, " \t")
	if strings.HasSuffix(trimmed, ":") {
		suffix = " :"
		rest = strings.TrimSuffix(trimmed, ":")
	}
	toks := strings.Fields(rest)
	if slot < 0 || slot >= len(toks) {
		return ch
	}
	tag := ""
	if i := strings.IndexByte(toks[slot], ':'); i >= 0 {
		tag = toks[slot][i:]
	}
	toks[slot] = chord + tag
	lead := ""
	if len(pre) == 0 && strings.HasPrefix(ch, " ") {
		lead = " "
	}
	trail := ""
	if strings.HasSuffix(ch, " ") && suffix == "" {
		trail = " "
	}
	return lead + pre + strings.Join(toks, " ") + suffix + trail
}

// Changes lists the header and grid lines that differ between two
// versions of a chart, section by section (never a lyric line).
func Changes(before, after string) []Change {
	var out []Change
	a, b := song.Parse(before), song.Parse(after)
	for _, k := range []string{"tempo", "time"} {
		if a.Get(k) != b.Get(k) {
			out = append(out, Change{Where: k, Before: a.Get(k), After: b.Get(k)})
		}
	}
	al, bl := sectionGrids(before), sectionGrids(after)
	// An intro added in front: compare the rest shifted by one.
	shift := 0
	if len(bl) == len(al)+1 && len(bl) > 0 && (len(al) == 0 || bl[0].label != al[0].label) {
		out = append(out, Change{Where: bl[0].label, Before: "", After: bl[0].grid})
		shift = 1
	}
	for i := 0; i+shift < len(bl) && i < len(al); i++ {
		if al[i].grid != bl[i+shift].grid {
			out = append(out, Change{Where: al[i].label, Before: al[i].grid, After: bl[i+shift].grid})
		}
	}
	return out
}

type secGrid struct{ label, grid string }

func sectionGrids(body string) []secGrid {
	lines := strings.Split(body, "\n")
	secs := sectionSpans(lines)
	count, seen := map[string]int{}, map[string]int{}
	for _, s := range secs {
		count[strings.TrimSpace(lines[s[0]])]++
	}
	var out []secGrid
	for _, s := range secs {
		h := strings.TrimSpace(lines[s[0]])
		label := strings.Trim(sectionHeaderRE.FindStringSubmatch(h)[1], " ")
		seen[h]++
		if count[h] > 1 {
			label = fmt.Sprintf("%s %d", label, seen[h])
		}
		var g []string
		for _, l := range lines[s[0]+1 : s[1]] {
			if isKey(l, "grid") || isKey(l, "times") {
				g = append(g, strings.TrimSpace(l))
			}
		}
		out = append(out, secGrid{label, strings.Join(g, " / ")})
	}
	return out
}
