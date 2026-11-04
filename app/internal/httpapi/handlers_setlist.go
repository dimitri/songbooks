package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"app/internal/setlist"
	"app/internal/store"
)

type jsonEntry struct {
	Book      string `json:"book"`
	Stem      string `json:"stem"`
	VariantID int64  `json:"variant_id,omitempty"`
	Note      string `json:"note"`
}

type jsonSet struct {
	Label   string      `json:"label"`
	Entries []jsonEntry `json:"entries"`
}

type jsonSetlist struct {
	Meta []jsonMetaField `json:"meta"`
	Sets []jsonSet       `json:"sets"`
}

func setlistToJSON(l *setlist.Setlist) jsonSetlist {
	out := jsonSetlist{}
	for _, f := range l.Meta {
		out.Meta = append(out.Meta, jsonMetaField{Key: f.Key, Value: f.Value})
	}
	for _, s := range l.Sets {
		js := jsonSet{Label: s.Label}
		for _, e := range s.Entries {
			js.Entries = append(js.Entries, jsonEntry{Book: e.Book, Stem: e.Stem, VariantID: e.VariantID, Note: e.Note})
		}
		out.Sets = append(out.Sets, js)
	}
	return out
}

func setlistFromJSON(in jsonSetlist) *setlist.Setlist {
	out := &setlist.Setlist{}
	for _, f := range in.Meta {
		out.Meta = append(out.Meta, setlist.MetaField{Key: f.Key, Value: f.Value})
	}
	for _, js := range in.Sets {
		s := setlist.Set{Label: js.Label}
		for _, e := range js.Entries {
			s.Entries = append(s.Entries, setlist.Entry{Book: e.Book, Stem: e.Stem, VariantID: e.VariantID, Note: e.Note})
		}
		out.Sets = append(out.Sets, s)
	}
	return out
}

// handleSetlistsPage lists this person's own setlists, plus every current
// band's setlists.
func (s *Server) handleSetlistsPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	mySetlists, err := s.store.ListSetlistsForPerson(r.Context(), person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	bands, err := s.store.ListBandsForPerson(r.Context(), person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type bandSetlists struct {
		Band     store.Band
		Setlists []store.SetlistMeta
	}
	var bandRows []bandSetlists
	for _, b := range bands {
		sls, err := s.store.ListSetlistsForBand(r.Context(), b.ID)
		if err != nil {
			continue
		}
		bandRows = append(bandRows, bandSetlists{Band: b, Setlists: sls})
	}

	s.render(w, "setlists-index", map[string]any{
		"Me": person,
		"MySetlists": mySetlists, "Bands": bandRows,
	})
}

// handleMetronomePage serves the standalone practice metronome. Normally
// no song attached, for warming up or drilling a groove before pulling up
// a chart — but the mobile Play page's "Metronome" bottom-bar entry links
// here with ?bpm=&time=&title=&back= so a song's own tempo/time signature
// preload instead of the generic default, and a "back to the song" link
// appears (the dedicated-page replacement for the old inline off-canvas
// metronome sheet on mobile — see play.html/app.css).
func (s *Server) handleMetronomePage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	q := r.URL.Query()
	s.render(w, "metronome", map[string]any{
		"Me":        person,
		"PresetBPM": q.Get("bpm"), "PresetTime": q.Get("time"),
		"BackTitle": q.Get("title"), "BackURL": q.Get("back"),
	})
}

// gigRow/gigSet are the "perform" view's model — just what a musician glances
// at before tapping a song: running number, title, key, tempo. Deliberately
// the same four columns as the band's own LaTeX titlepage set-list tables.
type gigRow struct {
	Num                                   int
	VariantID                             int64
	Title, Tempo, Key, Note, Reference string
}
type gigSet struct {
	Label string
	Rows  []gigRow
}

// handleSetlistOverview is GET /setlists/{id} — the setlist-by-numeric-ID
// family's own "view the actual set list" page: band.html's own setlist
// links used to point straight at /setlists/{id}/practice, which jumps
// into the FIRST song's full chart, not a list — a direct report ("the
// set list link should link to a view of the actual set list, not to the
// first song in it"). This is the missing middle page: the running order
// (reusing gigRow/gigSet, same fields the read-only gig page already
// shows), a print link to the existing one-pager PDF export, a Reorder
// toggle (client-side, saving through the existing PUT /api/setlists/{id}
// entries array), and links into the practice/listening/clicktrack views.
// A song's own reference: URL (if set) becomes that row's "play it" link.
func (s *Server) handleSetlistOverview(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	person := personFromContext(r)
	canView, err := s.store.CanViewSetlistByID(r.Context(), person.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !canView {
		httpError(w, http.StatusForbidden, "no view rights on this setlist")
		return
	}
	l, _, version, err := s.store.LoadSetlistByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	canEdit, _ := s.store.CanEditSetlistByID(r.Context(), person.ID, id)
	var sets []gigSet
	n := 0
	for _, set := range l.Sets {
		var rows []gigRow
		for _, e := range set.Entries {
			if !e.IsVariant() {
				continue
			}
			n++
			title, tempo, key, reference := "untitled", "", "", ""
			if sg, _, err := s.store.LoadVariant(e.VariantID); err == nil {
				title = orDefault(sg.Get("title"), "untitled")
				tempo = sg.Get("tempo")
				key = sg.Get("key")
				reference = sg.Get("reference")
			}
			rows = append(rows, gigRow{Num: n, VariantID: e.VariantID, Title: title, Tempo: tempo, Key: key, Note: e.Note, Reference: reference})
		}
		sets = append(sets, gigSet{Label: set.Label, Rows: rows})
	}
	s.render(w, "setlist-overview", map[string]any{
		"Me": person, "SetlistID": id, "Version": version, "CanEdit": canEdit,
		"Title": orDefault(l.Get("title"), "Setlist"), "Date": l.Get("date"), "Venue": l.Get("venue"),
		"Sets": sets,
	})
}

// --- person/band-scoped setlists (id-addressed), gigs, and
// practice/listening mode ------------------------------------------------

type jsonSetlistMeta struct {
	ID            int64  `json:"id"`
	Scope         string `json:"scope"`
	Notes         string `json:"notes"`
	Title         string `json:"title"`
	OwnerPersonID *int64 `json:"owner_person_id,omitempty"`
	OwnerBandID   *int64 `json:"owner_band_id,omitempty"`
	Version       string `json:"version"`
}

func setlistMetaToJSON(m *store.SetlistMeta) jsonSetlistMeta {
	return jsonSetlistMeta{
		ID: m.ID, Scope: m.Scope, Notes: m.Notes, Title: m.Title,
		OwnerPersonID: m.OwnerPersonID, OwnerBandID: m.OwnerBandID,
		Version: strconv.Itoa(m.Version),
	}
}

// handleCreateSetlist is POST /api/setlists — creates a new person- or
// band-scoped setlist (project-scoped setlists are still created via the
// existing PUT /api/setlists/{book}/{stem} upsert path, unchanged). For
// scope="band", the caller must be a CURRENT member of the given band —
// same open-collaboration rule as band repertoire; for scope="person",
// the new setlist is always owned by the caller themselves (no "create
// on someone else's behalf").
func (s *Server) handleCreateSetlist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope  string `json:"scope"`
		Title  string `json:"title"`
		Notes  string `json:"notes"`
		BandID int64  `json:"band_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	person := personFromContext(r)

	var ownerPersonID, ownerBandID *int64
	switch req.Scope {
	case "person":
		ownerPersonID = &person.ID
	case "band":
		if req.BandID == 0 {
			httpError(w, http.StatusBadRequest, "band_id is required for scope=band")
			return
		}
		isMember, err := s.store.IsCurrentBandMember(r.Context(), req.BandID, person.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isMember {
			httpError(w, http.StatusForbidden, "must be a current member of this band")
			return
		}
		ownerBandID = &req.BandID
	default:
		httpError(w, http.StatusBadRequest, `scope must be "person" or "band"`)
		return
	}

	id, err := s.store.CreateSetlist(r.Context(), req.Scope, req.Title, req.Notes, ownerPersonID, ownerBandID, person.ID)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// handleGetSetlistByID is GET /api/setlists/{id} — works for any scope,
// unlike the book/stem route above.
func (s *Server) handleGetSetlistByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid setlist id")
		return
	}
	person := personFromContext(r)
	canView, err := s.store.CanViewSetlistByID(r.Context(), person.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !canView {
		httpError(w, http.StatusForbidden, "no view rights on this setlist")
		return
	}
	l, meta, _, err := s.store.LoadSetlistByID(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"meta": setlistMetaToJSON(meta), "setlist": setlistToJSON(l),
	})
}

// handlePutSetlistByID is PUT /api/setlists/{id} — edits a person- or
// band-scoped setlist's title/notes fields and entries. Also works,
// deliberately, on a project-scoped setlist's id (an
// equivalent, id-addressed alternative to the existing book/stem PUT
// route) — CanEditSetlistByID enforces the same editor-role rule as
// requireRole("editor") does for that route, so this doesn't loosen
// anything for project-scoped setlists.
func (s *Server) handlePutSetlistByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid setlist id")
		return
	}
	person := personFromContext(r)
	canEdit, err := s.store.CanEditSetlistByID(r.Context(), person.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !canEdit {
		httpError(w, http.StatusForbidden, "no edit rights on this setlist")
		return
	}

	var req struct {
		Version string      `json:"version"`
		Title   string      `json:"title"`
		Notes   string      `json:"notes"`
		Setlist jsonSetlist `json:"setlist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	l := setlistFromJSON(req.Setlist)
	newVersion, err := s.store.SaveSetlistByID(r.Context(), id, l, req.Title, req.Notes, req.Version, person.ID)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			httpError(w, http.StatusConflict, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion})
}

// handleDeleteSetlistByID is DELETE /api/setlists/{id} — any scope,
// gated the same way edits are.
func (s *Server) handleDeleteSetlistByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid setlist id")
		return
	}
	person := personFromContext(r)
	canEdit, err := s.store.CanEditSetlistByID(r.Context(), person.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !canEdit {
		httpError(w, http.StatusForbidden, "no edit rights on this setlist")
		return
	}
	if err := s.store.DeleteSetlistByID(r.Context(), id); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// --- setlist_gigs --------------------------------------------------------

type jsonGig struct {
	ID    int64  `json:"id"`
	Date  string `json:"date,omitempty"` // "2026-06-01", or "" for TBD
	Venue string `json:"venue"`
	Notes string `json:"notes"`
}

func gigToJSON(g store.SetlistGig) jsonGig {
	jg := jsonGig{ID: g.ID, Venue: g.Venue, Notes: g.Notes}
	if g.Date != nil {
		jg.Date = g.Date.Format("2006-01-02")
	}
	return jg
}

// handleListSetlistGigs is GET /api/setlists/{id}/gigs.
func (s *Server) handleListSetlistGigs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid setlist id")
		return
	}
	person := personFromContext(r)
	canView, err := s.store.CanViewSetlistByID(r.Context(), person.ID, id)
	if err != nil || !canView {
		httpError(w, http.StatusForbidden, "no view rights on this setlist")
		return
	}
	gigs, err := s.store.ListSetlistGigs(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]jsonGig, 0, len(gigs))
	for _, g := range gigs {
		out = append(out, gigToJSON(g))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAddSetlistGig is POST /api/setlists/{id}/gigs — attaches an
// upcoming/past gig date to a (normally kind="performance", but not
// enforced — a themed/practice setlist could still gain a one-off gig
// row without harm) setlist. date is optional ("" = TBD).
func (s *Server) handleAddSetlistGig(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid setlist id")
		return
	}
	person := personFromContext(r)
	canEdit, err := s.store.CanEditSetlistByID(r.Context(), person.ID, id)
	if err != nil || !canEdit {
		httpError(w, http.StatusForbidden, "no edit rights on this setlist")
		return
	}
	var req struct {
		Date  string `json:"date"`
		Venue string `json:"venue"`
		Notes string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	var date *time.Time
	if req.Date != "" {
		t, err := time.Parse("2006-01-02", req.Date)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid date, expected YYYY-MM-DD")
			return
		}
		date = &t
	}
	gigID, err := s.store.AddSetlistGig(r.Context(), id, date, req.Venue, req.Notes)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": gigID})
}

// handleRemoveSetlistGig is DELETE /api/setlists/{id}/gigs/{gigID}.
func (s *Server) handleRemoveSetlistGig(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid setlist id")
		return
	}
	gigID, err := strconv.ParseInt(r.PathValue("gigID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid gig id")
		return
	}
	person := personFromContext(r)
	canEdit, err := s.store.CanEditSetlistByID(r.Context(), person.ID, id)
	if err != nil || !canEdit {
		httpError(w, http.StatusForbidden, "no edit rights on this setlist")
		return
	}
	if err := s.store.RemoveSetlistGig(r.Context(), gigID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// --- Practice / listening mode -------------------------------------------

// setlistStep is one resolved step of a practice/listening play-through.
type setlistStep struct {
	VariantID int64
	Note      string
}

// resolveSetlistSteps flattens every set's entries, in order, into a
// single run of variant-id steps; a non-variant (legacy book/stem) entry
// is skipped.
func (s *Server) resolveSetlistSteps(ctx context.Context, l *setlist.Setlist) []setlistStep {
	var out []setlistStep
	for _, set := range l.Sets {
		for _, e := range set.Entries {
			if !e.IsVariant() {
				continue
			}
			out = append(out, setlistStep{VariantID: e.VariantID, Note: e.Note})
		}
	}
	return out
}

// handleSetlistPractice is GET /setlists/{id}/practice — steps through
// the setlist's entries in order, full chord+lyric chart per song
// (reusing the normal single-song "song" template/render path, same as
// handleSetlistStep below). ?i=N picks the step (default 0).
func (s *Server) handleSetlistPractice(w http.ResponseWriter, r *http.Request) {
	s.handleSetlistStep(w, r, "practice")
}

// handleSetlistListening is GET /setlists/{id}/listening — same
// sequential step-through, but forced into the "song" page's existing
// lyrics-only view mode (large type, no chord grid) rather than the
// default chords+lyrics one.
func (s *Server) handleSetlistListening(w http.ResponseWriter, r *http.Request) {
	s.handleSetlistStep(w, r, "listening")
}

// handleSetlistStep is the shared practice/listening implementation: it
// resolves setlistID's entries to a run of steps, picks step i, and
// renders it with the exact same "song" template/data shape handleSong
// uses for a normal single-song page — buildSections, time-signature
// parsing, prev/next cards and all — just with Prev/Next pointed back
// into this same practice/listening run (via ?i=N) instead of at the
// project/setlist-gig neighbors handleSong itself offers.
func (s *Server) handleSetlistStep(w http.ResponseWriter, r *http.Request, mode string) {
	// no-store: this renders the same "song" template/chart as
	// handleSong, which got this header in 055d787 to defeat bfcache
	// restores serving a stale pre-deploy chart. This handler shares
	// the exact same chart-rendering path (see the doc comment above)
	// but had been missed, leaving practice/listening open to the same
	// stale-bfcache class of bug.
	w.Header().Set("Cache-Control", "no-store")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	person := personFromContext(r)
	canView, err := s.store.CanViewSetlistByID(r.Context(), person.ID, id)
	if err != nil || !canView {
		// Same "don't leak whether it exists" reasoning requireRole uses.
		http.NotFound(w, r)
		return
	}
	l, meta, _, err := s.store.LoadSetlistByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	steps := s.resolveSetlistSteps(r.Context(), l)
	if len(steps) == 0 {
		http.NotFound(w, r)
		return
	}
	i, _ := strconv.Atoi(r.URL.Query().Get("i"))
	if i < 0 {
		i = 0
	}
	if i >= len(steps) {
		i = len(steps) - 1
	}
	step := steps[i]
	variantID := step.VariantID

	sg, _, err := s.store.LoadVariant(variantID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sections := buildSections(sg)
	num, den := timeSigParts(orDefault(sg.Get("time"), "4/4"))
	var workID int64
	if v, err := s.store.GetVariant(r.Context(), variantID); err == nil {
		workID = v.WorkID
	}
	// Same implicit-fork-on-edit signal as handleSong — see its comment.
	canEdit, _ := s.store.CanEditVariant(r.Context(), person.ID, variantID)

	data := map[string]any{
		"Me": person,
		"VariantID": variantID, "WorkID": workID, "CanEdit": canEdit,
		"Title": orDefault(sg.Get("title"), "untitled"), "Artist": sg.Get("artist"),
		"Key": sg.Get("key"), "Flats": sg.Get("flats") == "yes", "Tempo": orDefault(sg.Get("tempo"), "120"), "Time": orDefault(sg.Get("time"), "4/4"),
		"TimeNum": num, "TimeDen": den,
		"Capo": sg.Get("capo"), "Reference": sg.Get("reference"),
		"TimeBeats": timeSigBeats(sg.Get("time")),
		"Sections":  sections,
		"MapLines":  buildMapLines(sections),

		"PracticeMode":      true,
		"PracticeStepMode":  mode, // "practice" or "listening"
		"PracticeSetlistID": id,
		"PracticeTitle":     orDefault(meta.Title, "set list"),
	}
	if i > 0 {
		if n := s.loadSongNeighbor(steps[i-1].VariantID); n != nil {
			data["Prev"] = n
			data["PrevIndex"] = i - 1
		}
	}
	if i < len(steps)-1 {
		if n := s.loadSongNeighbor(steps[i+1].VariantID); n != nil {
			data["Next"] = n
			data["NextIndex"] = i + 1
		}
	}
	s.render(w, "song", data)
}

// clicktrackSong is one flattened row for the setlist click-track mode —
// not the full chart (the point of this mode is the metronome, not the
// chart), just what the metronome widget needs to load this song's own
// tempo/time-signature and show where it sits in the running order.
type clicktrackSong struct {
	Index        int    `json:"index"`
	SetLabel     string `json:"setLabel"`
	SetIndex     int    `json:"setIndex"`
	IsFirstInSet bool   `json:"isFirstInSet"`
	VariantID    int64  `json:"variantId"`
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	Key          string `json:"key"`
	Tempo        int    `json:"tempo"`
	TimeSig      string `json:"timeSig"`
	Note         string `json:"note"`
}

// handleSetlistClicktrack is GET /setlists/{id}/clicktrack — the
// metronome (same widget/engine as the standalone page and Phase 30's
// advanced controls), pre-loaded with this setlist's songs in order.
// Unlike practice/listening (handleSetlistStep), this never renders a
// song's chart — the click is the point, not the chart — so it flattens
// every set's entries into a small JSON array (tempo/time-sig per song,
// set boundaries marked) for the client to step through without a page
// reload, the same way handleSetlistGigPage flattens a legacy book/stem
// setlist into gigRow/gigSet.
func (s *Server) handleSetlistClicktrack(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	person := personFromContext(r)
	canView, err := s.store.CanViewSetlistByID(r.Context(), person.ID, id)
	if err != nil || !canView {
		http.NotFound(w, r)
		return
	}
	l, meta, _, err := s.store.LoadSetlistByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var songs []clicktrackSong
	n := 0
	for si, set := range l.Sets {
		first := true
		for _, e := range set.Entries {
			if !e.IsVariant() {
				continue
			}
			sg, _, err := s.store.LoadVariant(e.VariantID)
			if err != nil {
				continue
			}
			tempo, _ := strconv.Atoi(orDefault(sg.Get("tempo"), "120"))
			if tempo <= 0 {
				tempo = 120
			}
			songs = append(songs, clicktrackSong{
				Index: n, SetLabel: set.Label, SetIndex: si, IsFirstInSet: first,
				VariantID: e.VariantID,
				Title:     orDefault(sg.Get("title"), "untitled"), Artist: sg.Get("artist"),
				Key: sg.Get("key"), Tempo: tempo, TimeSig: orDefault(sg.Get("time"), "4/4"),
				Note: e.Note,
			})
			n++
			first = false
		}
	}
	if len(songs) == 0 {
		http.NotFound(w, r)
		return
	}
	songsJSON, _ := json.Marshal(songs)

	s.render(w, "setlist-clicktrack", map[string]any{
		"Me": person, "SetlistID": id, "Title": orDefault(meta.Title, "set list"),
		"SongsJSON": template.JS(songsJSON),
	})
}
