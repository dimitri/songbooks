package httpapi

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"app/internal/chorddiagram"
	"app/internal/chordsym"
	"app/internal/gpfacts"
	"app/internal/song"
)

// driftItem is one fact the song's text and its Guitar Pro file both
// hold and disagree on (docs/gp-file-model.md §4). Nothing is ever fixed
// on its own: an editor uses the file's value (Take), pushes the text's
// into the file (Push, done in the browser by gp-drift.js, which writes
// a new file version), or keeps both (Kept, remembered in gp_drift_ack on
// these two values).
type driftItem struct {
	Fact  string     `json:"fact"`  // stable id: "tempo", "diagram:a7", "chords:2"
	Title string     `json:"title"` // "Tempo"
	Where string     `json:"where,omitempty"`
	Text  string     `json:"text"` // the text's value, "" when it has none
	File  string     `json:"file"` // the file's value, "" when it has none
	Sig   string     `json:"sig"`
	Kept  bool       `json:"kept"`
	Take  bool       `json:"take"`          // "Use the file's" applies
	Bar   int        `json:"bar,omitempty"` // the file's bar it's about, for the editor link
	Track string     `json:"track,omitempty"`
	Push  *driftPush `json:"push,omitempty"` // "Use this one" applies
}

// driftPush is what gp-drift.js writes into the file for "Use this one".
type driftPush struct {
	Kind   string `json:"kind"` // tempo, time, key, swing, diagram, marker
	Tempo  int    `json:"tempo,omitempty"`
	Num    int    `json:"num,omitempty"`
	Den    int    `json:"den,omitempty"`
	Sig    int    `json:"sig"`
	Minor  bool   `json:"minor,omitempty"`
	Swing  string `json:"swing"`
	Name   string `json:"name,omitempty"`
	Track  int    `json:"track,omitempty"` // 1-based, for a diagram
	Base   int    `json:"base,omitempty"`
	Frets  []int  `json:"frets,omitempty"` // from the highest string, as the facts
	Barres []int  `json:"barres,omitempty"`
	Bar    int    `json:"bar,omitempty"` // for a marker
}

func driftSig(text, file string) string {
	h := sha1.Sum([]byte(text + "\x00" + file))
	return hex.EncodeToString(h[:8])
}

// computeDrift compares sg (and its view sections) with the file's facts.
func computeDrift(sg *song.Song, sections []viewerSection, f *gpfacts.Facts, kept map[string]string) []driftItem {
	if f == nil {
		return nil
	}
	var out []driftItem
	add := func(it driftItem) {
		it.Sig = driftSig(it.Text, it.File)
		it.Kept = kept[it.Fact] == it.Sig
		out = append(out, it)
	}

	// Header facts. A song with no "tempo:"/"time:" is drawn at 120 and
	// 4/4, so those are what it says.
	if f.Tempo > 0 {
		text := strings.TrimSpace(sg.Get("tempo"))
		if n, err := strconv.Atoi(orDefault(text, "120")); err != nil || n != f.Tempo {
			p := &driftPush{Kind: "tempo", Tempo: n}
			if err != nil {
				p = nil
			}
			add(driftItem{Fact: "tempo", Title: "Tempo", Text: text, File: strconv.Itoa(f.Tempo), Take: true, Push: p})
		}
	}
	if len(f.Time) > 0 {
		ft := fmt.Sprintf("%d/%d", f.Time[0].Num, f.Time[0].Den)
		text := strings.TrimSpace(sg.Get("time"))
		if orDefault(text, "4/4") != ft {
			var p *driftPush
			if n, d := timeSigParts(text); text != "" {
				p = &driftPush{Kind: "time", Num: atoiOr(n, 4), Den: atoiOr(d, 4)}
			}
			add(driftItem{Fact: "time", Title: "Time signature", Text: text, File: ft, Take: true, Push: p})
		}
	}
	// Guitar Pro starts every file in C major: a file still in C says
	// nothing about the song's key.
	if ks, ok := f.KeyAt(1); ok && (ks.Sig != 0 || ks.Minor) {
		fk := gpfacts.KeyName(ks)
		text := strings.TrimSpace(sg.Get("key"))
		if text == "" || !chordsym.SameKey(text, fk) {
			var p *driftPush
			if sig, minor, ok := keySignature(text); ok {
				p = &driftPush{Kind: "key", Sig: sig, Minor: minor}
			}
			add(driftItem{Fact: "key", Title: "Key", Text: text, File: fk, Take: true, Push: p})
		}
	}
	if text := strings.TrimSpace(sg.Get("swing")); text != f.Swing && !(text == "" && f.Swing == "") {
		add(driftItem{Fact: "swing", Title: "Swing", Text: swingLabel(text), File: swingLabel(f.Swing), Take: true, Push: &driftPush{Kind: "swing", Swing: text}})
	}

	// Chord diagrams the file draws: the text should have the same shape.
	seen := map[string]bool{}
	for _, d := range f.Diagrams {
		name := gpfacts.ChordName(d.Name)
		spec, ok := d.Spec()
		if !ok || seen[gpfacts.Fold(name)] {
			continue
		}
		seen[gpfacts.Fold(name)] = true
		var mine *song.ChordRef
		for i := range sg.Chords {
			if chordsym.SameChord(sg.Chords[i].Name, name) {
				mine = &sg.Chords[i]
				break
			}
		}
		fact := "diagram:" + gpfacts.Fold(name)
		if mine == nil {
			add(driftItem{Fact: fact, Title: "Chord diagram " + name, Text: "", File: spec, Take: true})
			continue
		}
		if frets, base, barres := specFrets(mine.Spec); !sameInts(frets, d.Frets) {
			add(driftItem{Fact: fact, Title: "Chord diagram " + name, Text: mine.Spec, File: spec, Take: true,
				Push: &driftPush{Kind: "diagram", Name: name, Track: d.Track, Base: base, Frets: frets, Barres: barres}})
		}
	}

	// A chart next to its file: laid over it bar for bar (gpalign.go).
	alignDrift(sg, sections, f, add)

	// Sections that point into the file.
	for i := range sections {
		sec := &sections[i]
		si := sec.Index
		if si < 0 || si >= len(sg.Sections) || len(sg.Sections[si].GP) == 0 {
			continue
		}
		where := sectionName(sec)
		var first *gpfacts.Ref
		for k, r := range sg.Sections[si].GP {
			ref, err := gpfacts.ParseRef(r.Track, r.Spec)
			fact := fmt.Sprintf("ref:%d:%d", si, k)
			line := gpLine(r)
			switch {
			case err != nil:
				add(driftItem{Fact: fact, Title: "Bars from the file", Where: where, Text: line, File: err.Error()})
				continue
			case r.Track != "" && f.FindTrack(r.Track) < 0:
				add(driftItem{Fact: fact, Title: "Track", Where: where, Text: line, File: "tracks: " + trackNames(f)})
				continue
			}
			if last := ref.Ranges[len(ref.Ranges)-1][1]; last > f.Bars {
				add(driftItem{Fact: fact, Title: "Bars from the file", Where: where, Text: line, File: fmt.Sprintf("%d bars", f.Bars)})
				continue
			}
			if first == nil {
				first = &ref
			}
		}
		if first == nil {
			continue
		}
		played := f.PlayedBars(*first)
		if m := f.MarkerAt(first.Ranges[0][0]); m != "" && gpfacts.Fold(m) != gpfacts.Fold(sec.Label) {
			add(driftItem{Fact: fmt.Sprintf("marker:%d", si), Title: "Section name", Where: where, Text: sec.Label, File: m, Take: true, Bar: first.Ranges[0][0], Track: first.Track,
				Push: &driftPush{Kind: "marker", Name: sec.Label, Bar: first.Ranges[0][0]}})
		}
		if len(sg.Sections[si].Measures) == 0 {
			continue // its bars come from the file: nothing to disagree on
		}
		if sec.BarCount != len(played) {
			add(driftItem{Fact: fmt.Sprintf("bars:%d", si), Title: "Bar count", Where: where,
				Text: countOf(sec.BarCount, "bar"), File: countOf(len(played), "bar") + " (bars " + first.String() + ")", Take: true, Bar: first.Ranges[0][0], Track: first.Track})
			continue
		}
		if len(f.Chords) == 0 {
			continue
		}
		text := performedChordsAs(sections, i, func(s string) string { return strings.TrimSpace(s) })
		var differ []string
		firstBar := 0
		var tPrev, fPrev string
		var tShow, fShow []string
		for b, gpBar := range played {
			var tSeq, fSeq []string
			tSeq, tPrev = effectiveChords(text[b], tPrev, nil)
			var fc []gpfacts.BarChord
			if gpBar-1 < len(f.Chords) {
				fc = f.Chords[gpBar-1]
			}
			fSeq, fPrev = effectiveChords(barChordsJSON(fc), fPrev, gpfacts.ChordName)
			if !sameChordSeq(tSeq, fSeq) {
				if firstBar == 0 {
					firstBar = gpBar
				}
				differ = append(differ, strconv.Itoa(b+1))
				if len(tShow) < 4 {
					tShow = append(tShow, strings.Join(tSeq, " "))
					fShow = append(fShow, strings.Join(fSeq, " "))
				}
			}
		}
		if len(differ) > 0 {
			what := "bar " + differ[0]
			if len(differ) > 1 {
				what = "bars " + strings.Join(differ, ", ")
			}
			add(driftItem{Fact: fmt.Sprintf("chords:%d", si), Title: "Chords, " + what, Where: where,
				Text: strings.Join(tShow, " | "), File: strings.Join(fShow, " | "), Take: true, Bar: firstBar, Track: first.Track})
		}
	}
	return out
}

func barChordsJSON(in []gpfacts.BarChord) []mixerChordJSON {
	out := make([]mixerChordJSON, len(in))
	for i, c := range in {
		out[i] = mixerChordJSON{Pos: c.Pos, Name: c.Name}
	}
	return out
}

// effectiveChords is what sounds over one bar: the chord carried in from
// the bar before when nothing starts on the downbeat, then each change,
// repeats collapsed. prev is the chord in effect after the bar.
func effectiveChords(bar []mixerChordJSON, prev string, norm func(string) string) ([]string, string) {
	var seq []string
	push := func(n string) {
		if n == "" || (len(seq) > 0 && chordsym.SameChord(seq[len(seq)-1], n)) {
			return
		}
		seq = append(seq, n)
	}
	if len(bar) == 0 || bar[0].Pos > 0.01 {
		push(prev)
	}
	for _, c := range bar {
		n := c.Name
		if norm != nil {
			n = norm(n)
		}
		if n == "." || n == "%" || strings.EqualFold(n, "r") {
			continue
		}
		push(n)
	}
	if len(seq) > 0 {
		prev = seq[len(seq)-1]
	}
	return seq, prev
}

func sameChordSeq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !chordsym.SameChord(a[i], b[i]) {
			return false
		}
	}
	return true
}

// fileGrid writes the chords the file has over a ref's played bars as
// grid measures, the way the GP import does: one chord, or the chord
// carried in and the change, per bar; "." where nothing changes.
func fileGrid(f *gpfacts.Facts, ref gpfacts.Ref) []song.Measure {
	return fileGridBars(f, f.PlayedBars(ref))
}

// fileGridBars is fileGrid over a list of the file's bars, as played.
func fileGridBars(f *gpfacts.Facts, bars []int) []song.Measure {
	var out []song.Measure
	last := ""
	for _, b := range bars {
		var cs []gpfacts.BarChord
		if b-1 < len(f.Chords) {
			cs = f.Chords[b-1]
		}
		var names []string
		var firstPos float64
		for k, c := range cs {
			n := gpfacts.ChordName(c.Name)
			if k == 0 {
				firstPos = c.Pos
			}
			if len(names) == 0 || names[len(names)-1] != n {
				names = append(names, n)
			}
		}
		var tok []string
		switch {
		case len(names) > 1:
			first := names[0]
			if firstPos > 0.01 {
				first = "."
				if last == "" {
					first = "?"
				}
			} else if first == last {
				first = "."
			}
			tok = []string{first, names[len(names)-1]}
			last = names[len(names)-1]
		case len(names) == 1 && names[0] != last:
			tok = []string{names[0]}
			last = names[0]
		case last == "":
			tok = []string{"?"}
		default:
			tok = []string{"."}
		}
		out = append(out, song.Measure{Tokens: tok})
	}
	return out
}

// setGPStart writes "gp_start:", or takes it off for the file's first bar.
func setGPStart(sg *song.Song, n int) {
	if n <= 1 {
		sg.Unset("gp_start")
		return
	}
	sg.Set("gp_start", strconv.Itoa(n))
}

// keySignature is a key name's signature: sharps (> 0) or flats (< 0).
func keySignature(name string) (sig int, minor bool, ok bool) {
	k, ok := chordsym.ParseKey(name)
	if !ok {
		return 0, false, false
	}
	pc := k.PitchClass()
	if k.Minor {
		pc = (pc + 3) % 12
	}
	// Signatures by major tonic; F#/Gb follows how the root is spelled.
	bySharps := map[int]int{0: 0, 7: 1, 2: 2, 9: 3, 4: 4, 11: 5, 6: 6, 5: -1, 10: -2, 3: -3, 8: -4, 1: -5}
	sig = bySharps[pc]
	if pc == 6 && strings.Contains(k.Root, "b") {
		sig = -6
	}
	return sig, k.Minor, true
}

func swingLabel(s string) string {
	switch s {
	case "8":
		return "eighths"
	case "16":
		return "sixteenths"
	case "":
		return "straight"
	}
	return s
}

// specFrets reads a diagram spec as the facts write a shape: frets from
// the highest string, -1 muted, 0 open, plus its base and barre frets.
func specFrets(spec string) (frets []int, base int, barres []int) {
	d := chorddiagram.ParseSpec(spec)
	n := len(d.Strings)
	frets = make([]int, n)
	for i, sp := range d.Strings {
		v := -1
		switch sp.State {
		case chorddiagram.Open:
			v = 0
		case chorddiagram.Fretted:
			row := sp.Row
			// A string held only by a barre has no row of its own.
			for _, b := range d.Barres {
				if row == 0 && i >= b.FromString && i <= b.ToString {
					row = b.Row
				}
			}
			v = d.BaseFret + row - 1
		}
		frets[n-1-i] = v
	}
	for _, b := range d.Barres {
		barres = append(barres, d.BaseFret+b.Row-1)
	}
	return frets, d.BaseFret, barres
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sectionName(sec *viewerSection) string {
	name := sec.Label
	if sec.LabelNumber > 0 {
		name += " " + strconv.Itoa(sec.LabelNumber)
	}
	return name
}

func gpLine(r song.GPRef) string {
	if r.Track == "" {
		return "gp: " + r.Spec
	}
	return "gp[" + r.Track + "]: " + r.Spec
}

func trackNames(f *gpfacts.Facts) string {
	var names []string
	for _, t := range f.Tracks {
		names = append(names, strconv.Quote(t.Name))
	}
	return strings.Join(names, ", ")
}

func countOf(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return strconv.Itoa(n) + " " + what + "s"
}

// songDrift loads everything the drift check needs for one variant.
func (s *Server) songDrift(r *http.Request, variantID int64) (*song.Song, string, []driftItem, songGP, error) {
	sg, version, err := s.store.LoadVariant(variantID)
	if err != nil {
		return nil, "", nil, songGP{}, err
	}
	gp := s.loadSongGP(r.Context(), variantID)
	if gp.Facts == nil {
		return sg, version, nil, gp, nil
	}
	sections := buildSections(withGPOnlyBars(sg, gp.Facts))
	kept, _ := s.store.GPDriftAcks(r.Context(), variantID)
	return sg, version, computeDrift(sg, sections, gp.Facts, kept), gp, nil
}

// handleGetGPDrift is GET /api/songs/{id}/gp-drift.
func (s *Server) handleGetGPDrift(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	_, _, items, gp, err := s.songDrift(r, id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilDrift(items), "hasFile": gp.Meta != nil, "factsMissing": gp.Meta != nil && gp.Facts == nil})
}

func nonNilDrift(items []driftItem) []driftItem {
	if items == nil {
		return []driftItem{}
	}
	return items
}

// handlePostGPDriftKeep is POST /api/songs/{id}/gp-drift/keep {fact, sig}:
// keep a difference ("Keep both"); an empty sig stops keeping it.
func (s *Server) handlePostGPDriftKeep(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Fact string `json:"fact"`
		Sig  string `json:"sig"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Fact == "" || len(req.Fact) > 200 || len(req.Sig) > 64 {
		httpError(w, http.StatusBadRequest, "fact and sig expected")
		return
	}
	if err := s.store.AckGPDrift(r.Context(), id, req.Fact, req.Sig, actorID(r)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handlePostGPDriftTake is POST /api/songs/{id}/gp-drift/take {fact}:
// "Use the file's" — the text takes the file's value, saved as one
// revision.
func (s *Server) handlePostGPDriftTake(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Fact string `json:"fact"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	sg, version, items, gp, err := s.songDrift(r, id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	var it *driftItem
	for i := range items {
		if items[i].Fact == req.Fact && items[i].Take {
			it = &items[i]
		}
	}
	if it == nil {
		httpError(w, http.StatusConflict, "that difference is gone; reload the page")
		return
	}
	f := gp.Facts
	switch {
	case it.Fact == "tempo", it.Fact == "time", it.Fact == "key":
		sg.Set(it.Fact, it.File)
	case it.Fact == "swing":
		sg.Set("swing", f.Swing)
	case strings.HasPrefix(it.Fact, "diagram:"):
		name := strings.TrimPrefix(it.Title, "Chord diagram ")
		replaced := false
		for i := range sg.Chords {
			if chordsym.SameChord(sg.Chords[i].Name, name) {
				sg.Chords[i].Spec = it.File
				replaced = true
			}
		}
		if !replaced {
			sg.Chords = append(sg.Chords, song.ChordRef{Name: name, Spec: it.File})
		}
	case it.Fact == "start":
		if first := alignChart(sg, buildSections(withGPOnlyBars(sg, f)), f, gpStart(sg)).Sections; len(first) > 0 && first[0].Match != nil {
			setGPStart(sg, gpStart(sg)+first[0].Shift)
		}
	case strings.HasPrefix(it.Fact, "chords:") && !anyGPLine(sg):
		// Next to the file: the bars the section falls on from gp_start.
		si, _ := strconv.Atoi(strings.TrimPrefix(it.Fact, "chords:"))
		var at *viewerSection
		secs := buildSections(withGPOnlyBars(sg, f))
		for i := range secs {
			if secs[i].Index == si {
				at = &secs[i]
				break
			}
		}
		if at == nil || si < 0 || si >= len(sg.Sections) {
			httpError(w, http.StatusConflict, "that section is gone; reload the page")
			return
		}
		sg.Sections[si].Measures = fileGridBars(f, playedBarsOf(f, gpStart(sg), at.StartBar, at.BarCount))
		sg.Sections[si].Times = 0
	case strings.HasPrefix(it.Fact, "marker:"), strings.HasPrefix(it.Fact, "chords:"), strings.HasPrefix(it.Fact, "bars:"):
		si, _ := strconv.Atoi(it.Fact[strings.IndexByte(it.Fact, ':')+1:])
		if si < 0 || si >= len(sg.Sections) {
			httpError(w, http.StatusConflict, "that section is gone; reload the page")
			return
		}
		sec := &sg.Sections[si]
		if strings.HasPrefix(it.Fact, "marker:") {
			sec.Label = it.File
			break
		}
		for _, gr := range sec.GP {
			if ref, err := gpfacts.ParseRef(gr.Track, gr.Spec); err == nil {
				sec.Measures = fileGrid(f, ref)
				sec.Times = 0
				break
			}
		}
	default:
		httpError(w, http.StatusBadRequest, "this difference is fixed by hand")
		return
	}
	newVersion, err := s.store.SaveVariant(id, sg, version, actorID(r))
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion})
}
