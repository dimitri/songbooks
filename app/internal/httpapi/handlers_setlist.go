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

// handleSetlistsPage lists every setlist across every book — a setlist can
// reference songs from any book, so it isn't filed under a single book in
// the UI even though its file lives under one book's setlists/ directory.
func (s *Server) handleSetlistsPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	projects, err := s.store.ListProjectsForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var books []string
	for _, pr := range projects {
		books = append(books, pr.Slug)
	}
	type row struct{ Book, Stem string }
	var rows []row
	for _, b := range books {
		stems, err := s.store.ListSetlists(b)
		if err != nil {
			continue
		}
		for _, st := range stems {
			rows = append(rows, row{Book: b, Stem: st})
		}
	}

	// Phase 3: alongside the book/stem list above, also list this
	// person's own personal setlists and every current band's setlists —
	// scope='person'/'band' setlists have no book/stem, so they're their
	// own section rather than mixed into rows above.
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
		"Me": person, "Books": books, "Setlists": rows,
		"MySetlists": mySetlists, "Bands": bandRows,
	})
}

// handleMetronomePage serves the standalone practice metronome — no song
// attached, for warming up or drilling a groove before pulling up a chart.
func (s *Server) handleMetronomePage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	s.render(w, "metronome", map[string]any{"Me": person})
}

func (s *Server) handleSetlistEditPage(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	books, err := s.store.ListBooks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	songsByBook := map[string][]string{}
	for _, b := range books {
		songs, _ := s.store.ListSongs(b)
		songsByBook[b] = songs
	}
	songsJSON, _ := json.Marshal(songsByBook)
	s.render(w, "setlist-edit", map[string]any{
		"Book": book, "Stem": stem, "IsNew": stem == "new",
		"SongsByBookJSON": template.JS(songsJSON),
	})
}

// gigRow/gigSet are the "perform" view's model — just what a musician glances
// at before tapping a song: running number, title, key, tempo. Deliberately
// the same four columns as the band's own LaTeX titlepage set-list tables.
type gigRow struct {
	Num                                 int
	Book, Stem, Title, Tempo, Key, Note string
}
type gigSet struct {
	Label string
	Rows  []gigRow
}

// handleSetlistGigPage is the read-only, large-type, numbered-and-striped
// view meant to be used AT a gig or rehearsal — no editing controls, no
// drag handles, just the running order and enough per-song info (key,
// tempo) to orient before counting a song in. Tapping a row opens that
// song in the reader's own saved default view (song.js, localStorage),
// with ?gig=1 carried through so prev/next and the back link keep pointing
// back at this list instead of the editor.
func (s *Server) handleSetlistGigPage(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	l, _, err := s.store.LoadSetlist(book, stem)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var sets []gigSet
	n := 0
	for _, set := range l.Sets {
		var rows []gigRow
		for _, e := range set.Entries {
			n++
			title, tempo, key := e.Stem, "", ""
			if sg, _, err := s.store.LoadSong(e.Book, e.Stem); err == nil {
				title = orDefault(sg.Get("title"), e.Stem)
				tempo = sg.Get("tempo")
				key = sg.Get("key")
			}
			rows = append(rows, gigRow{Num: n, Book: e.Book, Stem: e.Stem, Title: title, Tempo: tempo, Key: key, Note: e.Note})
		}
		sets = append(sets, gigSet{Label: set.Label, Rows: rows})
	}
	s.render(w, "setlist-gig", map[string]any{
		"Book": book, "Stem": stem,
		"Title": orDefault(l.Get("title"), stem), "Date": l.Get("date"), "Venue": l.Get("venue"),
		"Sets": sets, "SetlistRef": book + "/" + stem,
	})
}

func (s *Server) handleGetSetlist(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	l, version, err := s.store.LoadSetlist(book, stem)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": version, "setlist": setlistToJSON(l)})
}

func (s *Server) handlePutSetlist(w http.ResponseWriter, r *http.Request) {
	book, stem := r.PathValue("book"), r.PathValue("stem")
	var req struct {
		Version string      `json:"version"`
		Setlist jsonSetlist `json:"setlist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	// A request body that decodes successfully but carries no "setlist"
	// field at all (e.g. a caller sending the wrong shape, as happened
	// once during manual testing of the gig view) would otherwise silently
	// overwrite real saved content with an empty file — reject it instead
	// of trusting a save with nothing in it.
	if len(req.Setlist.Meta) == 0 && len(req.Setlist.Sets) == 0 {
		httpError(w, http.StatusBadRequest, "request body has no setlist content — refusing to save an empty setlist")
		return
	}
	l := setlistFromJSON(req.Setlist)
	newVersion, err := s.store.SaveSetlist(book, stem, l, req.Version)
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

// --- Phase 3: person/band-scoped setlists (id-addressed), gigs, and
// practice/listening mode ------------------------------------------------

type jsonSetlistMeta struct {
	ID            int64  `json:"id"`
	Scope         string `json:"scope"`
	Notes         string `json:"notes"`
	Title         string `json:"title"`
	Book          string `json:"book,omitempty"`
	Stem          string `json:"stem,omitempty"`
	OwnerPersonID *int64 `json:"owner_person_id,omitempty"`
	OwnerBandID   *int64 `json:"owner_band_id,omitempty"`
	Version       string `json:"version"`
}

func setlistMetaToJSON(m *store.SetlistMeta) jsonSetlistMeta {
	return jsonSetlistMeta{
		ID: m.ID, Scope: m.Scope, Notes: m.Notes, Title: m.Title,
		Book: m.Book, Stem: m.Stem, OwnerPersonID: m.OwnerPersonID, OwnerBandID: m.OwnerBandID,
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

// setlistStep is one resolved step of a practice/listening play-through
// — a plain book/stem, resolved up front from either kind of Entry (see
// internal/setlist.Entry's doc comment) so the rest of the walk is
// identical regardless of how the setlist got there.
type setlistStep struct {
	Book, Stem, Note string
}

// resolveSetlistSteps flattens every set's entries, in order, into a
// single run of book/stem steps — a variant_id entry is resolved via
// GetVariant (every variant, person/band-owned included, still lives
// under some project — see store.ForkVariant's doc comment — so this
// always has a book/stem to resolve to); an entry that fails to resolve
// (a stale/deleted reference) is skipped rather than failing the whole
// page.
func (s *Server) resolveSetlistSteps(ctx context.Context, l *setlist.Setlist) []setlistStep {
	var out []setlistStep
	for _, set := range l.Sets {
		for _, e := range set.Entries {
			book, stem := e.Book, e.Stem
			if e.IsVariant() {
				v, err := s.store.GetVariant(ctx, e.VariantID)
				if err != nil {
					continue
				}
				book, stem = v.Book, v.Stem
			}
			if book == "" || stem == "" {
				continue
			}
			out = append(out, setlistStep{Book: book, Stem: stem, Note: e.Note})
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

	sg, _, err := s.store.LoadSong(step.Book, step.Stem)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sections := buildSections(sg)
	num, den := timeSigParts(orDefault(sg.Get("time"), "4/4"))
	variantID, workID, _ := s.store.VariantLookup(r.Context(), step.Book, step.Stem)
	// Same implicit-fork-on-edit signal as handleSong — see its comment.
	canEdit := true
	if variantID != 0 {
		canEdit, _ = s.store.CanEditVariant(r.Context(), person.ID, variantID)
	}

	data := map[string]any{
		"Book": step.Book, "Stem": step.Stem,
		"VariantID": variantID, "WorkID": workID, "CanEdit": canEdit,
		"Title": orDefault(sg.Get("title"), step.Stem), "Artist": sg.Get("artist"),
		"Key": sg.Get("key"), "Flats": sg.Get("flats") == "yes", "Tempo": orDefault(sg.Get("tempo"), "120"), "Time": orDefault(sg.Get("time"), "4/4"),
		"TimeNum": num, "TimeDen": den,
		"Capo": sg.Get("capo"), "Reference": sg.Get("reference"),
		"TimeBeats": timeSigBeats(sg.Get("time")),
		"Sections":  sections,

		"PracticeMode":      true,
		"PracticeStepMode":  mode, // "practice" or "listening"
		"PracticeSetlistID": id,
		"PracticeTitle":     orDefault(meta.Title, "set list"),
	}
	if i > 0 {
		ps := steps[i-1]
		if psg, _, err := s.store.LoadSong(ps.Book, ps.Stem); err == nil {
			data["Prev"] = &songNeighbor{Book: ps.Book, Stem: ps.Stem, Title: orDefault(psg.Get("title"), ps.Stem), Tempo: psg.Get("tempo"), Key: psg.Get("key")}
			data["PrevIndex"] = i - 1
		}
	}
	if i < len(steps)-1 {
		ns := steps[i+1]
		if nsg, _, err := s.store.LoadSong(ns.Book, ns.Stem); err == nil {
			data["Next"] = &songNeighbor{Book: ns.Book, Stem: ns.Stem, Title: orDefault(nsg.Get("title"), ns.Stem), Tempo: nsg.Get("tempo"), Key: nsg.Get("key")}
			data["NextIndex"] = i + 1
		}
	}
	s.render(w, "song", data)
}
