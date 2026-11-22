package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"app/internal/i18n"
	"app/internal/setlist"
	"app/internal/store"
)

type jsonEntry struct {
	ID        int64  `json:"id,omitempty"` // its setlist_items row; ignored on a save
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
			js.Entries = append(js.Entries, jsonEntry{ID: e.ID, Book: e.Book, Stem: e.Stem, VariantID: e.VariantID, Note: e.Note})
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
		"Me":         person,
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
	Num                                int
	VariantID                          int64
	Title, Tempo, Key, Note, Reference string
	Sound                              *setlistSound // nil: nothing to play here

	// The set list's own page: its setlist_items row, the version's name
	// ("psychedelic"), and the versions the owner's songbook has of the
	// song, for the editor to pick the one for this gig (none: nothing to
	// pick).
	ItemID      int64
	VersionName string
	Versions    []versionChoice
}

// versionChoice is one version a set list row may play, labelled so the
// versions of one song tell apart: "Bad Spell", "Bad Spell — psychedelic".
type versionChoice struct {
	VariantID int64
	Label     string
	Current   bool
}

// versionChoices labels vs for the picker of a row playing current: the
// title and version name, then the artist when two read the same, then
// the song's number.
func versionChoices(vs []store.SetlistVersion, current int64) []versionChoice {
	label := func(v store.SetlistVersion) string {
		if v.VersionName != "" {
			return v.Title + " — " + v.VersionName
		}
		return v.Title
	}
	seen := map[string]int{}
	for _, v := range vs {
		seen[label(v)]++
	}
	withArtist := map[string]int{}
	out := make([]versionChoice, 0, len(vs))
	for _, v := range vs {
		l := label(v)
		if seen[l] > 1 && v.Artist != "" {
			l += " · " + v.Artist
		}
		withArtist[l]++
		out = append(out, versionChoice{VariantID: v.VariantID, Label: l, Current: v.VariantID == current})
	}
	for i := range out {
		if withArtist[out[i].Label] > 1 {
			out[i].Label += fmt.Sprintf(" (%d)", out[i].VariantID)
		}
	}
	return out
}

// setlistSound is what a set list's player can play for a song: our
// recording (the band's chosen one from its pCloud folder, else the
// song's own) and the reference (the YouTube video's sound). The page
// plays the one the listener picks, the other when that's missing.
type setlistSound struct {
	Rec *soundSource
	Ref *soundSource
}

// soundSource is an mp3 of the app's (Src): the song's own, or the
// band's recording through the app (recordings_stream.go).
type soundSource struct {
	Src   string
	Label string
}

// soundFor is variantID's setlistSound for personID, nil when it has
// neither; bandID 0 for a set list that isn't a band's. The band's
// recording plays through the app, for who may hear it.
func (s *Server) soundFor(ctx context.Context, personID, bandID, variantID int64) *setlistSound {
	var snd setlistSound
	if bandID != 0 {
		if rec, err := s.store.ChosenSongRecording(ctx, bandID, variantID); err == nil && rec != nil && s.store.CanHearRecording(ctx, personID, rec.RecordingID) {
			snd.Rec = &soundSource{Src: recordingAudioURL(rec.RecordingID), Label: "the band's recording"}
		}
	}
	if audio, err := s.store.ListSongAudio(ctx, variantID); err == nil {
		for _, a := range audio {
			src := fmt.Sprintf("/song/%d/audio.mp3?category=%s&v=%d", variantID, a.Category, a.ByteSize)
			switch {
			case a.Category == store.AudioBand && snd.Rec == nil:
				snd.Rec = &soundSource{Src: src, Label: "our recording"}
			case a.Category == store.AudioYouTube:
				snd.Ref = &soundSource{Src: src, Label: "the reference"}
			}
		}
	}
	if snd.Rec == nil && snd.Ref == nil {
		return nil
	}
	return &snd
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
// entries array), and a link to the click track.
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
	l, meta, version, err := s.store.LoadSetlistByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.atSetlistPath(w, r, meta, "") {
		return
	}
	brandKind, brandID := setlistOwner(meta)
	canEdit, _ := s.store.CanEditSetlistByID(r.Context(), person.ID, id)
	backURL, backLabel := "/catalog?view=setlists", "Set lists"
	var band *store.Band
	var bandID int64
	if meta.OwnerBandID != nil {
		bandID = *meta.OwnerBandID
		backURL = fmt.Sprintf("/bands/%d/setlists", bandID)
		if b, err := s.store.GetBand(r.Context(), bandID); err == nil {
			backLabel, band = b.Name, b
		}
	}
	lang := s.langOf(w)
	playable, choice := 0, false
	var sets []gigSet
	n := 0
	// Which version each row plays, and, for whoever edits the set list,
	// the others its owner's songbook has (docs/ownership-model.md §5).
	versionNames, err := s.store.VersionNames(r.Context(), store.SetlistVariantIDs(l))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var choices map[int64][]store.SetlistVersion
	if canEdit {
		if choices, err = s.store.SetlistVersionChoices(r.Context(), id); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	for _, set := range l.Sets {
		var rows []gigRow
		for _, e := range set.Entries {
			if !e.IsVariant() {
				continue
			}
			n++
			title, tempo, key, reference := i18n.T(lang, "untitled"), "", "", ""
			if sg, _, err := s.store.LoadVariant(e.VariantID); err == nil {
				title = orDefault(sg.Get("title"), i18n.T(lang, "untitled"))
				tempo = sg.Get("tempo")
				key = sg.Get("key")
				reference = sg.Get("reference")
			}
			row := gigRow{Num: n, VariantID: e.VariantID, Title: title, Tempo: tempo, Key: key, Note: e.Note, Reference: reference,
				ItemID: e.ID, VersionName: versionNames[e.VariantID]}
			if vs := choices[e.ID]; len(vs) > 1 {
				row.Versions = versionChoices(vs, e.VariantID)
			}
			if row.Sound = s.soundFor(r.Context(), personIDOf(r), bandID, e.VariantID); row.Sound != nil {
				playable++
				choice = choice || row.Sound.Rec != nil && row.Sound.Ref != nil
			}
			rows = append(rows, row)
		}
		sets = append(sets, gigSet{Label: set.Label, Rows: rows})
	}
	// The songs that can be added: a band's set list picks from the
	// band's repertoire, listed here; any other one searches the catalog.
	var repertoire []map[string]any
	if canEdit && meta.OwnerBandID != nil {
		entries, err := s.store.ListBandCatalog(r.Context(), *meta.OwnerBandID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		inList := map[int64]bool{}
		for _, set := range l.Sets {
			for _, e := range set.Entries {
				inList[e.VariantID] = true
			}
		}
		bandName := ""
		if b, err := s.store.GetBand(r.Context(), *meta.OwnerBandID); err == nil {
			bandName = b.Name
		}
		for _, e := range entries {
			if inList[e.VariantID] {
				continue
			}
			artist := e.Artist
			if artist == bandName {
				artist = "" // the band's own songs: the artist says nothing
			}
			title := e.Title
			if e.VersionName != "" {
				title += " — " + e.VersionName // one row per version: which one
			}
			repertoire = append(repertoire, map[string]any{"variantId": e.VariantID, "title": title, "artist": artist})
		}
	}
	// The set list's owner's songbook (§4: a set list lists songs of it):
	// the Add / Create dialog links a song there before it joins the list.
	var ownerSongbookID int64
	if canEdit {
		o := meta.Owner()
		if sbID, err := s.store.GetDefaultSongbook(r.Context(), o.Kind, o.ID); err == nil {
			ownerSongbookID = sbID
		}
	}
	s.render(w, "setlist-overview", map[string]any{
		"Me": person, "SetlistID": id, "Version": version, "CanEdit": canEdit, "OwnerSongbookID": ownerSongbookID,
		"Path": setlistPath(meta), "BackURL": backURL, "BackLabel": backLabel,
		"Title": orDefault(l.Get("title"), orDefault(meta.Title, i18n.T(lang, "Set list"))), "Date": l.Get("date"), "Venue": l.Get("venue"),
		"IsEmpty": n == 0, "Repertoire": repertoire, "IsBand": meta.OwnerBandID != nil,
		"Sets": sets, "BrandKind": brandKind, "BrandID": brandID,
		// Its exports are in the page's own toolbar (Export ▾), not the
		// sidebar: actions on the set list, not places to go.
		"Band": band, "Playable": playable, "Choice": choice,
	})
}

// --- person/band-scoped setlists (id-addressed) and gigs ------------------

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
		Scope       string `json:"scope"`
		Title       string `json:"title"`
		Notes       string `json:"notes"`
		BandID      int64  `json:"band_id"`
		CommunityID int64  `json:"community_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	person := personFromContext(r)

	var ownerPersonID, ownerBandID, ownerCommunityID *int64
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
	case "community":
		if req.CommunityID == 0 {
			httpError(w, http.StatusBadRequest, "community_id is required for scope=community")
			return
		}
		canEdit, err := s.store.HasCommunityPrivilege(r.Context(), req.CommunityID, person.ID, store.PrivEditRepertoire)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !canEdit {
			httpError(w, http.StatusForbidden, "your role in this community doesn't grant setlist creation")
			return
		}
		ownerCommunityID = &req.CommunityID
	default:
		httpError(w, http.StatusBadRequest, `scope must be "person", "band", or "community"`)
		return
	}

	id, err := s.store.CreateSetlist(r.Context(), req.Scope, req.Title, req.Notes, ownerPersonID, ownerBandID, ownerCommunityID, person.ID)
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
	// A band's set list holds the band's own songs: what was there before
	// this save, to tell the songs it adds.
	before, meta, _, err := s.store.LoadSetlistByID(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A band's set list holds what the band may link (a public song, its
	// own): another band's private song, or a closed songbook's, is
	// refused before anything is saved, with the catalogue's version
	// offered when there is one (writeLinkErr).
	if meta.Scope == "band" && meta.OwnerBandID != nil {
		had := map[int64]bool{}
		for _, v := range store.SetlistVariantIDs(before) {
			had[v] = true
		}
		for _, v := range store.SetlistVariantIDs(l) {
			if had[v] {
				continue
			}
			if err := s.store.LinkCheckFor(r.Context(), person.ID, v, store.BandOwner(*meta.OwnerBandID)); err != nil && !errors.Is(err, store.ErrNotFound) {
				s.writeLinkErr(w, r, err)
				return
			}
		}
	}
	newVersion, err := s.store.SaveSetlistByID(r.Context(), id, l, req.Title, req.Notes, req.Version, person.ID)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			httpError(w, http.StatusConflict, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if meta.Scope == "band" && meta.OwnerBandID != nil {
		if err := s.linkSetlistAdditions(r.Context(), *meta.OwnerBandID, before, l, person); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion})
}

// handlePutSetlistItemVersion is PUT /api/setlists/{id}/items/{itemID},
// body {"variant_id": N, "version": "7"}: the song row itemID plays the
// version N for this set list — one of the versions its owner's songbook
// has of that song (SetlistVersionChoices; anything else is a 400). Whoever
// may edit the set list may; a set list changed since "version" is a 409.
func (s *Server) handlePutSetlistItemVersion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid setlist id")
		return
	}
	itemID, err := strconv.ParseInt(r.PathValue("itemID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid set list row id")
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
		VariantID int64  `json:"variant_id"`
		Version   string `json:"version"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	version, err := s.store.ChooseSetlistVersion(r.Context(), person.ID, id, itemID, req.VariantID, req.Version)
	switch {
	case errors.Is(err, store.ErrConflict):
		httpError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, store.ErrInvalid):
		httpError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": version})
}

// linkSetlistAdditions puts each song a save added to bandID's set list
// in the band's songbook, as a link, when none of the band's songbooks
// holds it yet: a set list lists songs of its band's songbook
// (docs/ownership-model.md §4). Nothing is copied: Edit on the song makes
// the band's version.
func (s *Server) linkSetlistAdditions(ctx context.Context, bandID int64, before, after *setlist.Setlist, person *store.Person) error {
	had := map[int64]bool{}
	for _, v := range store.SetlistVariantIDs(before) {
		had[v] = true
	}
	for _, v := range store.SetlistVariantIDs(after) {
		if had[v] {
			continue
		}
		in, err := s.store.InOwnerSongbooks(ctx, store.BandOwner(bandID), v)
		if err != nil {
			return err
		}
		if in {
			continue
		}
		if err := s.store.LinkCheckFor(ctx, person.ID, v, store.BandOwner(bandID)); err != nil {
			continue // checked before the save; a song gone since
		}
		if err := s.store.AddBandCatalogEntry(ctx, bandID, v, person.ID); err != nil {
			return err
		}
	}
	return nil
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

// setlistPath is where a set list's page lives: inside its band
// (/bands/{band}/setlists/{id}) when a band owns it, so the band's look,
// back links and tabs stay around it; at /setlists/{id} otherwise.
func setlistPath(meta *store.SetlistMeta) string {
	if meta.OwnerBandID != nil {
		return fmt.Sprintf("/bands/%d/setlists/%d", *meta.OwnerBandID, meta.ID)
	}
	return fmt.Sprintf("/setlists/%d", meta.ID)
}

// atSetlistPath sends a request for a set list's page (suffix "" or
// "/clicktrack") to its setlistPath when it came in by another one —
// /setlists/{id} for a band's set list, or another band's URL — and
// reports whether the request is already there.
func (s *Server) atSetlistPath(w http.ResponseWriter, r *http.Request, meta *store.SetlistMeta, suffix string) bool {
	want := setlistPath(meta) + suffix
	if r.URL.Path == want {
		return true
	}
	if r.URL.RawQuery != "" {
		want += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, want, http.StatusMovedPermanently)
	return false
}

// handleSetlistOldStep is GET /setlists/{id}/practice and /listening:
// the practice and listening step-throughs are gone (a song opened from
// the running order already steps through the set with Previous/Next),
// so links people kept land on the running order.
func (s *Server) handleSetlistOldStep(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/setlists/%d", id), http.StatusMovedPermanently)
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
	Note         string `json:"note"`      // the set list entry's own note
	ClickNote    string `json:"clickNote"` // the click-track note, shown large while it plays

	// How the click counts this time signature (clicktrack_meter.go).
	Meter clickMeter `json:"meter"`

	// Lead is how many bars the song plays before its bar 1 (a "lead:"
	// line, song.Song.LeadIn): the click counts them first, as 0 (and -1,
	// …), so its bar 1 is the song's.
	Lead int `json:"lead,omitempty"`
}

// handleSetlistClicktrack is GET /setlists/{id}/clicktrack — the
// metronome (same widget/engine as the standalone page and Phase 30's
// advanced controls), pre-loaded with this setlist's songs in order.
// It never renders a
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
	if !s.atSetlistPath(w, r, meta, "/clicktrack") {
		return
	}

	canEdit, err := s.store.CanEditSetlistByID(r.Context(), person.ID, id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	clickNotes, err := s.store.ListClicktrackNotes(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	lang := s.langOf(w)
	var songs []clicktrackSong
	n, playable := 0, 0
	var bandID int64
	if meta.OwnerBandID != nil {
		bandID = *meta.OwnerBandID
	}
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
				Title:     orDefault(sg.Get("title"), i18n.T(lang, "untitled")), Artist: sg.Get("artist"),
				Key: sg.Get("key"), Tempo: tempo, TimeSig: orDefault(sg.Get("time"), "4/4"),
				Note: e.Note, ClickNote: clickNotes[e.VariantID],
				Meter: clickMeterFor(orDefault(sg.Get("time"), "4/4")),
				Lead:  sg.LeadIn(),
			})
			n++
			first = false
			// Listen, among the set list's views: only when a song has
			// something to play.
			if playable == 0 && s.soundFor(r.Context(), personIDOf(r), bandID, e.VariantID) != nil {
				playable++
			}
		}
	}
	if len(songs) == 0 {
		http.NotFound(w, r)
		return
	}
	songsJSON, _ := json.Marshal(songs)

	s.render(w, "setlist-clicktrack", map[string]any{
		"Me": person, "SetlistID": id, "Title": orDefault(meta.Title, i18n.T(lang, "set list")), "Path": setlistPath(meta),
		"SongsJSON": template.JS(songsJSON), "CanEdit": canEdit, "Playable": playable > 0,
	})
}

// handlePutClicktrackNote is PUT /api/setlists/{id}/clicktrack-notes/
// {variantID}, body {"note": "..."}: saves the click-track note for one
// song of the set list (an empty note removes it). Whoever may edit the
// set list may edit its notes, and only for a song that is in it.
func (s *Server) handlePutClicktrackNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid setlist id")
		return
	}
	variantID, err := strconv.ParseInt(r.PathValue("variantID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song id")
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
	l, _, _, err := s.store.LoadSetlistByID(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	inList := false
	for _, set := range l.Sets {
		for _, e := range set.Entries {
			if e.VariantID == variantID {
				inList = true
			}
		}
	}
	if !inList {
		httpError(w, http.StatusNotFound, "this song is not in the set list")
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	note, err := s.store.SetClicktrackNote(r.Context(), id, variantID, req.Note, person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"note": note})
}

// personIDOf is the signed-in person's id, 0 for no one.
func personIDOf(r *http.Request) int64 {
	if p := personFromContext(r); p != nil {
		return p.ID
	}
	return 0
}
