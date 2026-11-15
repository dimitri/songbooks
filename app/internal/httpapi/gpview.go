package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"app/internal/gpfacts"
	"app/internal/song"
	"app/internal/store"
)

// viewerGPRef is one "gp[Track]: bars" line as the song page draws it: a
// placeholder static/js/gp-view.js fills with those bars of the song's
// Guitar Pro file.
type viewerGPRef struct {
	Track      string // as written; "" for every track
	TrackIndex int    // 0-based index in the file, -1 for every track or unknown
	Bars       string // "9-12,13-14", for data-bars
	FirstBar   int    // the first bar it shows, for the editor link
	Notation   string // "tab", "staff", "tab+staff", "slash", or "" for the reader's own
	Label      string // "Lead guitar · bars 9–12"
	Error      string // why it can't be drawn, for editors
}

// songGP is a song's Guitar Pro file as the song page needs it.
type songGP struct {
	Meta  *store.SongGPSourceFile // nil when the song has no file
	Facts *gpfacts.Facts          // nil until read
}

func (s *Server) loadSongGP(ctx context.Context, variantID int64) songGP {
	meta, err := s.store.GetSongGPSourceFileMeta(ctx, variantID)
	if err != nil {
		return songGP{}
	}
	facts, _ := gpfacts.Parse(meta.Facts)
	return songGP{Meta: meta, Facts: facts}
}

// isGPOnly reports whether the song says its content is all in its file.
func isGPOnly(sg *song.Song) bool {
	return strings.EqualFold(strings.TrimSpace(sg.Get("content")), "gp")
}

// withGPOnlyBars gives a section that only points into the file (no chord
// bars, no score of its own) as many empty bars as its first reference
// plays, so its bar count and start bar are right for everything that
// counts bars (the structure strip, the Mixer, recordings). The song
// itself is left untouched, as withRiffOnlyBars does.
func withGPOnlyBars(sg *song.Song, facts *gpfacts.Facts) *song.Song {
	if facts == nil {
		return sg
	}
	var out *song.Song
	for i, sec := range sg.Sections {
		if len(sec.GP) == 0 || len(sec.Measures) > 0 || len(sec.Scores) > 0 || sec.Repeat {
			continue
		}
		ref, err := gpfacts.ParseRef(sec.GP[0].Track, sec.GP[0].Spec)
		if err != nil {
			continue
		}
		n := len(facts.PlayedBars(ref))
		if n == 0 {
			continue
		}
		if out == nil {
			cp := *sg
			cp.Sections = append([]song.Section(nil), sg.Sections...)
			out = &cp
		}
		out.Sections[i].Measures = make([]song.Measure, n)
	}
	if out == nil {
		return sg
	}
	return out
}

// attachGPRefs puts each section's "gp[...]" lines on its view, checked
// against the file's facts when they're known.
func attachGPRefs(sections []viewerSection, sg *song.Song, gp songGP) {
	for i := range sections {
		si := sections[i].Index
		if si < 0 || si >= len(sg.Sections) {
			continue
		}
		src := sg.Sections[si]
		if len(src.GP) == 0 {
			continue
		}
		sections[i].GPOnly = len(src.Measures) == 0 && len(src.Scores) == 0
		for _, r := range src.GP {
			sections[i].GP = append(sections[i].GP, viewGPRef(r, gp))
		}
	}
}

func viewGPRef(r song.GPRef, gp songGP) viewerGPRef {
	v := viewerGPRef{Track: r.Track, TrackIndex: -1}
	ref, err := gpfacts.ParseRef(r.Track, r.Spec)
	v.Notation = ref.Notation
	var bars []string
	for _, rg := range ref.Ranges {
		bars = append(bars, fmt.Sprintf("%d-%d", rg[0], rg[1]))
	}
	v.Bars = strings.Join(bars, ",")
	if len(ref.Ranges) > 0 {
		v.FirstBar = ref.Ranges[0][0]
	}
	what := r.Track
	if what == "" {
		what = "All tracks"
	}
	v.Label = what + " · bars " + strings.ReplaceAll(ref.String(), "-", "–")
	if ref.Notation != "" {
		v.Label = strings.TrimSuffix(v.Label, " "+ref.Notation)
	}
	switch {
	case err != nil:
		v.Error = err.Error()
	case gp.Meta == nil:
		v.Error = "this song has no Guitar Pro file"
	case gp.Facts != nil:
		if r.Track != "" {
			v.TrackIndex = gp.Facts.FindTrack(r.Track)
			if v.TrackIndex < 0 {
				v.Error = "the file has no track " + strconv.Quote(r.Track)
			}
		}
		for _, rg := range ref.Ranges {
			if rg[1] > gp.Facts.Bars {
				v.Error = fmt.Sprintf("the file has %d bars", gp.Facts.Bars)
			}
		}
	}
	return v
}

// handlePutSectionGP is PUT /api/songs/{id}/sections/{index}/gp
// {track, spec}: sets the section's "gp[Track]:" line for that track (the
// GP import wizard), replacing one already there for it and dropping the
// notes an earlier import copied into the text as a "score[Track]:"
// voice of the same name.
func (s *Server) handlePutSectionGP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	var req struct {
		Track string `json:"track"`
		Spec  string `json:"spec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	ref, err := gpfacts.ParseRef(req.Track, req.Spec)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	sg, version, err := s.store.LoadVariant(id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if index < 0 || index >= len(sg.Sections) {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	setSectionGP(&sg.Sections[index], ref.Track, ref.String())
	newVersion, err := s.store.SaveVariant(id, sg, version, actorID(r))
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion})
}

// setSectionGP sets sec's "gp[track]:" line and drops a "score[track]:"
// voice of the same name (an earlier import's copy of those notes).
func setSectionGP(sec *song.Section, track, spec string) {
	replaced := false
	for i := range sec.GP {
		if gpfacts.Fold(sec.GP[i].Track) == gpfacts.Fold(track) {
			sec.GP[i].Spec = spec
			replaced = true
		}
	}
	if !replaced {
		sec.GP = append(sec.GP, song.GPRef{Track: track, Spec: spec})
	}
	kept := sec.Scores[:0]
	for _, sv := range sec.Scores {
		if gpfacts.Fold(sv.Label) != gpfacts.Fold(track) {
			kept = append(kept, sv)
		}
	}
	sec.Scores = kept
}

// handleGPEditPage is GET /song/{id}/gp-edit: the Guitar Pro file editor
// (gp-editor.js). ?track= and ?bar= put the cursor there; a song with no
// file yet starts a blank one as long as the song (?parts=guitar,bass,drums).
func (s *Server) handleGPEditPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
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
	q := r.URL.Query()
	data := map[string]any{
		"Me": personFromContext(r), "VariantID": variantID,
		"Title": orDefault(sg.Get("title"), "untitled"), "Artist": sg.Get("artist"),
		"Track": q.Get("track"), "Bar": q.Get("bar"),
	}
	gp := s.loadSongGP(r.Context(), variantID)
	data["SectionsJSON"] = editorSections(sg, gp)
	data["PickerSectionsJSON"] = pickerSections(sg, gp)
	if gp.Meta != nil {
		data["GPFileName"] = gp.Meta.Filename
	} else {
		num, den := timeSigParts(orDefault(sg.Get("time"), "4/4"))
		bars := 0
		for _, sec := range buildSections(sg) {
			bars += sec.BarCount
		}
		parts := []string{}
		for _, p := range strings.Split(q.Get("parts"), ",") {
			if p == "guitar" || p == "bass" || p == "drums" {
				parts = append(parts, p)
			}
		}
		if len(parts) == 0 {
			parts = []string{"guitar"}
		}
		data["Blank"] = true
		data["Tempo"] = atoiOr(orDefault(sg.Get("tempo"), "120"), 120)
		data["Num"], data["Den"] = atoiOr(num, 4), atoiOr(den, 4)
		data["Bars"] = max(bars, 4)
		data["Parts"] = strings.Join(parts, ",")
	}
	s.render(w, "gp-edit", data)
}

// editorSection is a song section the editor shows the name of, on the
// bars of the file it points at.
type editorSection struct {
	Index int      `json:"index"`
	Label string   `json:"label"`
	Track string   `json:"track"`
	Bars  [][2]int `json:"bars"`
}

// editorSections lists, as JSON, the song's sections with their
// "gp[...]" lines: the editor names the section a bar belongs to, and
// links back to it.
func editorSections(sg *song.Song, gp songGP) string {
	var out []editorSection
	for _, sec := range buildSections(withGPOnlyBars(sg, gp.Facts)) {
		if sec.Index < 0 || sec.Index >= len(sg.Sections) {
			continue
		}
		for _, r := range sg.Sections[sec.Index].GP {
			ref, err := gpfacts.ParseRef(r.Track, r.Spec)
			if err != nil {
				continue
			}
			out = append(out, editorSection{Index: sec.Index, Label: sectionName(&sec), Track: r.Track, Bars: ref.Ranges})
		}
	}
	b, _ := json.Marshal(out)
	if out == nil {
		return "[]"
	}
	return string(b)
}

// handleDeleteSectionGP is DELETE /api/songs/{id}/sections/{index}/gp?track=:
// drops the section's "gp[track]:" line ("" for the bare "gp:" one).
func (s *Server) handleDeleteSectionGP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	track := r.URL.Query().Get("track")
	sg, version, err := s.store.LoadVariant(id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if index < 0 || index >= len(sg.Sections) {
		httpError(w, http.StatusBadRequest, "invalid section index")
		return
	}
	sec := &sg.Sections[index]
	kept := sec.GP[:0]
	for _, g := range sec.GP {
		if gpfacts.Fold(g.Track) != gpfacts.Fold(track) {
			kept = append(kept, g)
		}
	}
	sec.GP = kept
	newVersion, err := s.store.SaveVariant(id, sg, version, actorID(r))
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion})
}

// pickerSection is a song section as the bar picker (gp-bar-picker.js)
// needs it: its name, how many bars it plays (nil when its bars come from
// the file), and its "gp[...]" lines.
type pickerSection struct {
	Index    int         `json:"index"`
	Label    string      `json:"label"`
	BarCount *int        `json:"barCount"`
	GP       []jsonGPRef `json:"gp"`
}

// pickerSections lists every section of the song for the bar picker, as
// JSON for a data- attribute.
func pickerSections(sg *song.Song, gp songGP) string {
	out := []pickerSection{}
	sections := buildSections(withGPOnlyBars(sg, gp.Facts))
	attachGPRefs(sections, sg, gp)
	for _, sec := range sections {
		if sec.Index < 0 || sec.Index >= len(sg.Sections) {
			continue
		}
		ps := pickerSection{Index: sec.Index, Label: sectionName(&sec), GP: []jsonGPRef{}}
		if !sec.GPOnly {
			n := sec.BarCount
			ps.BarCount = &n
		}
		for _, g := range sg.Sections[sec.Index].GP {
			ps.GP = append(ps.GP, jsonGPRef{Track: g.Track, Spec: g.Spec})
		}
		out = append(out, ps)
	}
	b, _ := json.Marshal(out)
	return string(b)
}
