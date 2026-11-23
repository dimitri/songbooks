package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"app/internal/chordsym"
	"app/internal/covers"
	"app/internal/i18n"
	"app/internal/store"
	"app/internal/webassets"
)

// handleMySongbooksPage lists the logged-in person's own songbooks (the
// "project list"/My songbook page's replacement) — every person has at
// least a default one (see migration 0039).
func (s *Server) handleMySongbooksPage(w http.ResponseWriter, r *http.Request) {
	p := personFromContext(r)
	books, err := s.store.ListSongbooks(r.Context(), "person", p.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "my-songbooks", map[string]any{"Me": p, "Songbooks": books})
}

// handleCreateSongbook makes a new songbook for the logged-in person, or
// (ownerBandID given) for a band they're a current member of — "a band
// could choose to have a songbook per album, or a single songbook with
// all the songs in it."
func (s *Server) handleCreateSongbook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string `json:"name"`
		OwnerBandID      int64  `json:"ownerBandId"`
		OwnerCommunityID int64  `json:"ownerCommunityId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "missing name")
		return
	}
	p := personFromContext(r)
	ownerKind, ownerID := "person", p.ID
	switch {
	case req.OwnerBandID != 0:
		isMember, err := s.store.IsCurrentBandMember(r.Context(), req.OwnerBandID, p.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isMember {
			httpError(w, http.StatusForbidden, "must be a current member of this band")
			return
		}
		ownerKind, ownerID = "band", req.OwnerBandID
	case req.OwnerCommunityID != 0:
		canEdit, err := s.store.HasCommunityPrivilege(r.Context(), req.OwnerCommunityID, p.ID, store.PrivEditRepertoire)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !canEdit {
			httpError(w, http.StatusForbidden, "your role in this community doesn't grant songbook creation")
			return
		}
		ownerKind, ownerID = "community", req.OwnerCommunityID
	}
	id, err := s.store.CreateSongbook(r.Context(), ownerKind, ownerID, strings.TrimSpace(req.Name), false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

// handleSongbookPage shows one songbook: its song list (ordered per its
// own song_order, same Set 1/Set 2/... grouping a project used to have),
// playlist link, and (for a band songbook) the usual band-page chrome.
func (s *Server) handleSongbookPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	ok, err := s.store.CanAccessSongbook(r.Context(), p.ID, id)
	if err != nil || !ok {
		http.NotFound(w, r)
		return
	}
	sb, err := s.store.GetSongbook(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// A course opens on its lessons; ?view=songs is its songs as a list.
	if sb.IsCourse() && r.URL.Query().Get("view") != "songs" {
		s.handleCoursePage(w, r, sb)
		return
	}
	groups, err := s.store.ListSongsOrdered(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type songbookSongRow struct {
		VariantID         int64
		Title, Tempo, Key string
		// VersionName tells a version from the owner's other versions of
		// the same song: one row each (docs/ownership-model.md §5).
		VersionName string
		Cover             *covers.Cover
		// The key the song is kept in here (songbook_keys.go), and the
		// keys a row's control offers, for whoever can change the songbook.
		Semitones int
		Keys      []keyOption
		// Voice: the song's melody at this key, and how it sits in the
		// student's voice (row_voice.go).
		Voice rowVoice
	}
	canKey := false
	canEdit, _ := s.store.CanEditSongbook(r.Context(), p.ID, id)
	if canEdit && !sb.IsStudent() && sb.Kind != store.SongbookKindGroup {
		canKey = true
	}
	entryKeys, err := s.store.SongbookEntryKeys(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type songbookGroup struct {
		Label string
		Songs []songbookSongRow
	}
	// The covers: the banner's look, and one by each song.
	songCovers, err := s.store.SongbookCovers(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	coverOf := map[int64]*covers.Cover{}
	for _, sc := range songCovers {
		coverOf[sc.VariantID] = sc.Cover
		if !sc.Looked {
			s.fillCovers(id)
		}
	}
	var look *store.BookLook
	ownerName := ""
	if l := store.SongbookLook(songCovers); len(l.Mosaic) >= 4 {
		look = &l
		switch sb.OwnerKind {
		case "band":
			_, ownerName, _ = s.store.GetBrand(r.Context(), "band", sb.OwnerBandID)
		case "community":
			_, ownerName, _ = s.store.GetBrand(r.Context(), "community", sb.OwnerCommunityID)
		}
	}
	var allIDs []int64
	for _, g := range groups {
		allIDs = append(allIDs, g.VariantIDs...)
	}
	versionNames, err := s.store.VersionNames(r.Context(), allIDs)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ranges := s.melodyRanges(r.Context(), allIDs)
	zone := s.songbookVoiceZone(r.Context(), p.ID, sb)
	var out []songbookGroup
	for _, g := range groups {
		sg := songbookGroup{Label: g.Label}
		if g.Lesson != nil { // a course's lesson, named in the page's language
			sg.Label = g.Lesson.Label(s.langOf(w))
		}
		for _, vid := range g.VariantIDs {
			row := songbookSongRow{VariantID: vid, Title: i18n.T(s.langOf(w), "untitled"), Cover: coverOf[vid], VersionName: versionNames[vid]}
			ownKey := ""
			if parsed, _, err := s.store.LoadVariant(vid); err == nil {
				row.Title = orDefault(parsed.Get("title"), row.Title)
				row.Tempo = parsed.Get("tempo")
				row.Key = parsed.Get("key")
				ownKey = songKeyName(parsed)
			}
			row.Semitones = entryKeys[vid]
			if row.Semitones != 0 {
				if k := chordsym.KeyIn(ownKey, row.Semitones); k != "" {
					row.Key = k
				}
			}
			if canKey {
				row.Keys = keyOptions(s.langOf(w), ownKey, row.Semitones)
			}
			m, known := ranges[vid]
			row.Voice = rowVoiceOf(s.langOf(w), m, known, row.Semitones, zone)
			sg.Songs = append(sg.Songs, row)
		}
		out = append(out, sg)
	}
	contextual := webassets.ContextualGroup{
		Label:          "This songbook",
		PDFExportPath:  fmt.Sprintf("/songbooks/%d/export-songbook.pdf", id),
		PDFExportLabel: "Songbook PDF",
		Entries: []webassets.NavEntry{
			{Label: "Metronome playlist", Path: fmt.Sprintf("/songbooks/%d/playlist/metronome", id), Icon: "icon-metro-full"},
			{Label: "Playlist", Path: fmt.Sprintf("/songbooks/%d/playlist", id), Icon: "icon-play"},
		},
	}
	if sb.PlaylistURL != "" {
		contextual.Entries = append([]webassets.NavEntry{
			{Label: "Playlist", Path: sb.PlaylistURL, Icon: "icon-reference", External: true},
		}, contextual.Entries...)
	}
	var ids []int64
	for _, g := range groups {
		ids = append(ids, g.VariantIDs...)
	}
	// A person's songbook: their own copies of a song a band of theirs
	// has its own version of, tagged (store.BandVersionsOf).
	bandVersions := map[int64]*store.BandVersion{}
	if sb.OwnerKind == "person" {
		if m, err := s.store.BandVersionsOf(r.Context(), p.ID, ids); err == nil {
			bandVersions = m
		}
	}
	s.render(w, "songbook", map[string]any{
		"Me": p, "Songbook": sb, "Groups": out, "Badges": s.songBadgesFor(r.Context(), ids),
		"ContextualGroup": contextual, "Look": look, "OwnerName": ownerName,
		"BandVersions": bandVersions, "CanEdit": canEdit,
	})
}

// handleRenameSongbook renames any songbook (default or not) the caller
// can access.
func (s *Server) handleRenameSongbook(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanEditSongbook(r.Context(), p.ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no edit access to this songbook")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "missing name")
		return
	}
	if err := s.store.RenameSongbook(r.Context(), id, strings.TrimSpace(req.Name)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleDeleteSongbook deletes a non-default songbook the caller can
// access — "every profile should have a 'my song book' default... can't
// have zero" means the default one is never deletable (store.DeleteSongbook
// enforces this).
func (s *Server) handleDeleteSongbook(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanEditSongbook(r.Context(), p.ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no edit access to this songbook")
		return
	}
	if err := s.store.DeleteSongbook(r.Context(), id); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleSaveSongbookOrder persists a songbook's song order (drag-reorder
// on the songbook page).
func (s *Server) handleSaveSongbookOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanEditSongbook(r.Context(), p.ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no edit access to this songbook")
		return
	}
	var req struct {
		Groups []struct {
			Label      string  `json:"label"`
			VariantIDs []int64 `json:"variantIds"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	groups := make([]store.SongGroup, len(req.Groups))
	for i, g := range req.Groups {
		groups[i] = store.SongGroup{Label: g.Label, VariantIDs: g.VariantIDs}
	}
	if err := s.store.SaveOrder(r.Context(), id, groups); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleCreateSongInSongbook is the Add / Create dialog's Create
// (docs/ownership-model.md §1: a song is always created in a songbook of
// its owner): {title, artist, referenceUrl, public} makes a new song owned
// by songbookID's owner, in that songbook only (store.CreateSongIn).
// Private unless public is asked and the person may publish there (their
// own; a band's core members; a community's staff; never a closed
// songbook).
func (s *Server) handleCreateSongInSongbook(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	var req struct {
		Title        string `json:"title"`
		Artist       string `json:"artist"`
		ReferenceURL string `json:"referenceUrl"`
		Public       bool   `json:"public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	variantID, err := s.store.CreateSongIn(r.Context(), p.ID, id, strings.TrimSpace(req.Title), strings.TrimSpace(req.Artist),
		strings.TrimSpace(req.ReferenceURL), req.Public)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "no such songbook")
		return
	case errors.Is(err, store.ErrForbidden) && req.Public:
		httpError(w, http.StatusForbidden, i18n.T(s.langOfRequest(r), "You can’t make songs public in this songbook."))
		return
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, "no edit access to this songbook")
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"variantId": variantID})
}

// handleCreateCatalogSong creates a brand-new catalogue song (The Big
// Songbook's, public), for a site admin; the result isn't added to any
// songbook automatically — add it to one explicitly via
// handleAddSongbookEntry, same as any other catalogue song.
func (s *Server) handleCreateCatalogSong(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title        string `json:"title"`
		Artist       string `json:"artist"`
		ReferenceURL string `json:"referenceUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		httpError(w, http.StatusBadRequest, "missing title")
		return
	}
	// The catalogue is The Big Songbook's: its staff, the site's admins,
	// write and publish it (docs/ownership-model.md §2, §6); everyone
	// else writes a song in a songbook of their own, and may make it
	// public there.
	if p := personFromContext(r); p == nil || !p.IsAdmin {
		httpError(w, http.StatusForbidden, "only the site's admins add to the catalogue")
		return
	}
	variantID, err := s.store.CreateVariant(r.Context(), store.OwnerCatalog, 0, strings.TrimSpace(req.Title), strings.TrimSpace(req.Artist), actorID(r))
	if err == nil && strings.TrimSpace(req.ReferenceURL) != "" {
		err = s.store.SetCatalogueReference(r.Context(), variantID, strings.TrimSpace(req.ReferenceURL))
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"variantId": variantID})
}

// handleAddSongbookEntry links a song to songbookID (store.LinkCheck).
func (s *Server) handleAddSongbookEntry(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanEditSongbook(r.Context(), p.ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no edit access to this songbook")
		return
	}
	var req struct {
		VariantID int64 `json:"variantId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VariantID == 0 {
		httpError(w, http.StatusBadRequest, "missing variantId")
		return
	}
	// A link (docs/ownership-model.md §4): a band's or a community's
	// songbook links public songs and its own; a person's, anything they
	// see; a closed songbook's song goes nowhere else (store.LinkCheck).
	sb, err := s.store.GetSongbook(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such songbook")
		return
	}
	if !s.writeLinkErr(w, r, s.store.LinkCheck(r.Context(), p.ID, req.VariantID, sb)) {
		return
	}
	variantID := s.coachingSongFor(r.Context(), sb, req.VariantID)
	if err := s.store.AddToSongbook(r.Context(), id, variantID, p.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": true, "variantId": variantID})
}

// handleRemoveSongbookEntry removes a variant pointer from songbookID —
// never deletes the variant itself.
func (s *Server) handleRemoveSongbookEntry(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	variantID, err := pathID(r, "variantID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	p := personFromContext(r)
	if ok, err := s.store.CanEditSongbook(r.Context(), p.ID, id); err != nil || !ok {
		httpError(w, http.StatusForbidden, "no edit access to this songbook")
		return
	}
	if err := s.store.RemoveFromSongbook(r.Context(), id, variantID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// handleDeleteSong permanently deletes a variant (not just a songbook
// pointer to it) — gated at the router by requireVariantRole("editor",
// ...), same bar as editing it. Every other owner linking it keeps a
// frozen copy (store.DeleteSong).
func (s *Server) handleDeleteSong(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteSong(r.Context(), id, personFromContext(r).ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleRedoSong is the destructive "redo this song from scratch" action:
// resets an existing variant's body back to the blank template a brand-
// new song starts from — same id, same row, same revision history, but
// every bit of content discarded.
func (s *Server) handleRedoSong(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	text, newVersion, err := s.store.RedoVariant(id, req.Title, req.Version, actorID(r))
	if errors.Is(err, store.ErrConflict) {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion, "text": text})
}

// keyOption is one key of a songbook row's key control: the song moved by
// N semitones, named ("A (−2)"), or its own key.
type keyOption struct {
	N        int
	Label    string
	Selected bool
}

// keyOptions are the keys a songbook row offers, an octave's span around
// the song's own (ownKey, "" when it has none), the one it's in selected.
func keyOptions(lang, ownKey string, current int) []keyOption {
	var out []keyOption
	for n := -6; n <= 6; n++ {
		label := ""
		if n == 0 {
			if ownKey != "" {
				label = i18n.T(lang, "%s (own key)", ownKey)
			} else {
				label = i18n.T(lang, "its own key")
			}
		} else {
			off := strings.Replace(fmt.Sprintf("%+d", n), "-", "−", 1)
			if k := chordsym.KeyIn(ownKey, n); k != "" {
				label = k + " (" + off + ")"
			} else {
				label = off
			}
		}
		out = append(out, keyOption{N: n, Label: label, Selected: n == current})
	}
	return out
}

// handleSetSongbookPublic is POST /api/songbooks/{id}/public {public}: the
// songbook made public — everyone signed in opens it, and its owner's
// songs in it go public with it — or private again (its songs stay as
// they are). A person their own; a band's core members; a community's
// staff (store.SongbookRights.Publish); never a closed songbook.
func (s *Server) handleSetSongbookPublic(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	var req struct {
		Public bool `json:"public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	switch err := s.store.SetSongbookPublic(r.Context(), personFromContext(r).ID, id, req.Public); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, "you can't publish this songbook")
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"public": req.Public})
	}
}
