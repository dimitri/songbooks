package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"app/internal/i18n"
	"app/internal/songdiff"
	"app/internal/store"
)

// handleCatalogPage is the global catalog browse view: every global
// variant plus the viewer's own, filtered by artist/project and sorted
// alphabetically or by artist. "By album" is intentionally not offered —
// no song in this app's data model carries an album field (see
// song.MetaKeys in internal/song/song.go), so there's nothing to group or
// filter by; "by project" is the closest grouping concept that actually
// exists in the data.
//
// It is the Library (menu PR 8): its Songs segment, and ?view=songbooks
// or ?view=setlists for the other two (the old /songbooks and /setlists
// pages, which redirect here).
func (s *Server) handleCatalogPage(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("view") {
	case libraryViewSongbooks:
		s.handleSongbooksIndexPage(w, r)
		return
	case libraryViewSetlists:
		s.handleSetlistsPage(w, r)
		return
	}
	person := personFromContext(r)
	lang := s.langOf(w)
	// Everything the person sees (docs/ownership-model.md §1): their own
	// songs, their bands' and communities', every public one, each with
	// whose it is and who sees it, the versions of a work together.
	filter := store.FindFilter{
		Query:  r.URL.Query().Get("q"),
		Scope:  validFindScope(r.URL.Query().Get("scope")),
		Artist: r.URL.Query().Get("artist"),
		Genre:  r.URL.Query().Get("genre"),
		Sort:   r.URL.Query().Get("sort"),
	}
	works, err := s.store.FindSongs(r.Context(), person.ID, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	genres, err := s.store.ListGenres(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var ids []int64
	owners := map[int64]string{}
	for _, wk := range works {
		for _, sg := range wk.Songs {
			ids = append(ids, sg.ID)
			owners[sg.ID] = ownerBadge(lang, sg)
		}
	}
	type scopeView struct {
		Label, Href string
		Active      bool
	}
	var scopes []scopeView
	for _, f := range findScopes {
		v := url.Values{}
		for _, k := range []string{"q", "artist", "genre", "sort"} {
			if x := r.URL.Query().Get(k); x != "" {
				v.Set(k, x)
			}
		}
		if f.Scope != "" {
			v.Set("scope", f.Scope)
		}
		href := "/catalog"
		if len(v) > 0 {
			href += "?" + v.Encode()
		}
		scopes = append(scopes, scopeView{Label: f.Label, Href: href, Active: f.Scope == filter.Scope})
	}
	// The bands whose recordings of a song everyone hears
	// (docs/ownership-model.md §8): "played by: Mercy (2 recordings)".
	playedBy, _ := s.store.PlayedByFor(r.Context(), ids)
	s.render(w, "catalog", map[string]any{
		"Me": person, "Works": works, "Filter": filter, "Genres": genres, "Badges": s.songBadgesFor(r.Context(), ids),
		"Owners": owners, "Scopes": scopes, "PlayedBy": playedBy,
	})
}

// catalogSearchHit is one result of handleCatalogSearch — just enough to
// render a type-ahead row and carry the variant id the caller actually
// needs (e.g. the band repertoire "Add song" dialog's add/fork actions).
type catalogSearchHit struct {
	VariantID  int64  `json:"variantId"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Visibility string `json:"visibility"`
	// Band: the band's name, for a band's version shown in place of the
	// catalogue's song (store.PreferBandVersions).
	Band string `json:"band,omitempty"`
}

// handleCatalogSearch is GET /api/catalog/search?q=... — a JSON type-
// ahead over the same ListCatalog full-text search handleCatalogPage
// uses, for any UI that needs "find a variant by name" without a full
// page navigation (the band repertoire "Add song" dialog's by-id form
// used to make someone go find a catalog page, copy a numeric id, and
// paste it back — this replaces that with search-as-you-type). Capped
// at maxCatalogSearchResults since it's meant for a dropdown, not a
// browse page.
const maxCatalogSearchResults = 20

func (s *Server) handleCatalogSearch(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []catalogSearchHit{})
		return
	}
	variants, err := s.store.ListCatalog(r.Context(), person.ID, store.CatalogFilter{Query: q})
	if err == nil {
		// ?band=N: a band's add dialog — its own version, never another
		// band's (a band doesn't take another band's song).
		onlyBand, _ := strconv.ParseInt(r.URL.Query().Get("band"), 10, 64)
		variants, err = s.store.PreferBandVersionsFor(r.Context(), person.ID, variants, onlyBand)
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(variants) > maxCatalogSearchResults {
		variants = variants[:maxCatalogSearchResults]
	}
	hits := make([]catalogSearchHit, len(variants))
	for i, v := range variants {
		hits[i] = catalogSearchHit{VariantID: v.VariantID, Title: v.Title, Artist: v.Artist, Visibility: v.Visibility, Band: v.BandName}
	}
	writeJSON(w, http.StatusOK, hits)
}

// handleSongbooksIndexPage is the "Songbooks" nav entry's destination: every
// songbook the viewer can open, their own personal ones plus each of their
// bands' shared repertoires — split out from the flat all-songs catalog
// (handleCatalogPage) so each has its own URL/nav entry instead of one page
// with an internal tab toggle.
func (s *Server) handleSongbooksIndexPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	mySongbooks, err := s.store.ListSongbooks(r.Context(), "person", person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bands, err := s.store.ListBandsForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bandCards, _, err := s.buildBandCards(r.Context(), s.langOf(w), person.ID, bands)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Community (community) repertoires were missing from this index
	// entirely — a community member had no way to reach their
	// community's own shared songbook from the main Songbooks page,
	// only the bands+personal sections existed. Member count mirrors
	// bandCardView's own "N members" meta line.
	communities, err := s.store.ListCommunitiesForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type communityCardView struct {
		ID      int64
		Name    string
		Members int
	}
	var communityCards []communityCardView
	for _, sc := range communities {
		members, err := s.store.ListCommunityMembers(r.Context(), sc.ID, false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		communityCards = append(communityCards, communityCardView{ID: sc.ID, Name: sc.Name, Members: len(members)})
	}
	s.render(w, "songbooks-index", map[string]any{
		"Me": person, "MySongbooks": mySongbooks, "BandCards": bandCards, "CommunityCards": communityCards,
	})
}

// handleWorkVersionsPage is a work's "this song’s recordings and arrangements" page:
// every variant the viewer may see (every global one, plus their own
// private ones), with a free-text annotation box/list underneath.
func (s *Server) handleWorkVersionsPage(w http.ResponseWriter, r *http.Request) {
	workID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid work id", http.StatusBadRequest)
		return
	}
	person := personFromContext(r)
	variants, err := s.store.ListVariantsForWork(r.Context(), workID, person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Annotations are per-variant, not per-work — fetch each variant's
	// notes so the page can show them grouped under their variant.
	type variantWithNotes struct {
		store.CatalogVariant
		Annotations []store.Annotation
		CanCopy     bool // not a private songbook's song
	}
	var rows []variantWithNotes
	for _, v := range variants {
		notes, err := s.store.ListAnnotations(r.Context(), v.VariantID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		canCopy, err := s.store.CanCopyVariant(r.Context(), person.ID, v.VariantID, "user", 0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rows = append(rows, variantWithNotes{CatalogVariant: v, Annotations: notes, CanCopy: canCopy})
	}
	s.render(w, "work_versions", map[string]any{
		"Me": person, "WorkID": workID, "Variants": rows,
	})
}

// handleVariantHistoryPage is one variant's revision history, with a
// revert button on every past revision.
func (s *Server) handleVariantHistoryPage(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid variant id", http.StatusBadRequest)
		return
	}
	person := personFromContext(r)
	variant, err := s.store.GetVariant(r.Context(), variantID)
	if err != nil {
		// A song merged into another (§7): its history is on that one's.
		if !s.mergedRedirect(w, r, variantID) {
			http.NotFound(w, r)
		}
		return
	}
	revisions, err := s.store.ListRevisions(r.Context(), "variant", strconv.FormatInt(variantID, 10))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rights, err := s.store.SongRightsFor(r.Context(), person.ID, variantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	canEdit := rights.Edit
	// Which edits the viewer may revert: any, for who edits the song; on
	// The Big Songbook's, per catalogueRevertable.
	revertable := map[int64]bool{}
	if rights.Catalogue {
		revertable = s.catalogueRevertable(r.Context(), person.ID, revisions)
	} else if canEdit {
		for _, rv := range revisions {
			revertable[rv.ID] = true
		}
	}
	// The Guitar Pro files the song was imported from, current and earlier
	// (best effort: a failure just leaves the section out).
	gpCurrent, _ := s.store.GetSongGPSourceFileMeta(r.Context(), variantID)
	gpVersions, _ := s.store.ListSongGPSourceFileVersions(r.Context(), variantID)
	// The songs merged into this one (§7), each with its own history.
	type mergedHistory struct {
		store.SongMerge
		Revisions []store.Revision
	}
	var merged []mergedHistory
	if ms, err := s.store.MergedInto(r.Context(), variantID); err == nil {
		for _, m := range ms {
			revs, _ := s.store.ListRevisions(r.Context(), "variant", strconv.FormatInt(m.MergedSongID, 10))
			merged = append(merged, mergedHistory{SongMerge: m, Revisions: revs})
		}
	}
	s.render(w, "variant_history", map[string]any{
		"Me": person, "Variant": variant, "Revisions": revisions, "CanEdit": canEdit, "Merged": merged,
		"Revertable": revertable, "Catalogue": rights.Catalogue, "CanContribute": rights.Contribute,
		"GPFile": gpCurrent, "GPVersions": gpVersions,
		"Edits": s.describeEdits(variantID, revisions),
	})
}

// handleForkVariant is "Make my arrangement" (docs/ownership-model.md §5):
// the person's own version of a song they see (never a closed songbook's),
// in its place in their songbooks ({"songbookId"}, else every one of
// theirs linking it; their default songbook when none does).
func (s *Server) handleForkVariant(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	var req struct {
		SongbookID int64 `json:"songbookId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	person := personFromContext(r)
	newID, err := s.store.MakeVersion(r.Context(), person.ID, variantID, store.PersonOwner(person.ID), req.SongbookID, "")
	if !s.writeLinkErr(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"variantId": newID})
}

// handleNewVersion is POST /api/variants/{id}/versions {name, songbookId}:
// "New arrangement from this" (docs/ownership-model.md §5) — another version
// of a song the caller edits, owned by the same owner, named ("Bad Spell —
// psychedelic"), as a new row beside it in {songbookId} (else in each of
// the owner's songbooks holding it). Answers its id.
func (s *Server) handleNewVersion(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	var req struct {
		Name       string `json:"name"`
		SongbookID int64  `json:"songbookId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, i18n.T(s.langOfRequest(r), "A new version needs a name."))
		return
	}
	ctx := r.Context()
	person := personFromContext(r)
	rights, err := s.store.SongRightsFor(ctx, person.ID, variantID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !rights.Edit || rights.Catalogue {
		httpError(w, http.StatusForbidden, "you can't make a version of this song here")
		return
	}
	newID, err := s.store.MakeVersion(ctx, person.ID, variantID, rights.Owner, req.SongbookID, req.Name)
	if !s.writeLinkErr(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"variantId": newID})
}

// handleSetSongPublic is POST /api/variants/{id}/public {public}: the song
// made public (everyone signed in sees it, links it, makes their own
// version of it; everything goes with it: its chart, reference,
// recordings, stems, Guitar Pro file) or private. A person their own; a
// band's core members; a community's staff; the site's admins for The Big
// Songbook's (store.SongRights.Publish). Never a closed songbook's song.
func (s *Server) handleSetSongPublic(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	var req struct {
		Public bool `json:"public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	switch err := s.store.SetSongPublic(r.Context(), personFromContext(r).ID, variantID, req.Public); {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, "you can't publish this song")
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"public": req.Public})
	}
}

// handleCreateAnnotation leaves a free-text note on a variant — not an
// edit to its body/grid/lyrics, a comment alongside it.
func (s *Server) handleCreateAnnotation(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
		httpError(w, http.StatusBadRequest, "missing body")
		return
	}
	person := personFromContext(r)
	id, err := s.store.AnnotateVariant(r.Context(), variantID, person.ID, req.Body)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handleListRevisions is GET /api/revisions?entity_type=X&entity_id=Y —
// generic revision history for any entity type this phase (or a later
// one) records revisions for.
func (s *Server) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	entityType := r.URL.Query().Get("entity_type")
	entityID := r.URL.Query().Get("entity_id")
	if entityType == "" || entityID == "" {
		httpError(w, http.StatusBadRequest, "entity_type and entity_id are required")
		return
	}
	revisions, err := s.store.ListRevisions(r.Context(), entityType, entityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, revisions)
}

// handleRevertRevision is POST /api/revisions/{id}/revert. Per the
// phase-1 brief, anyone with edit rights on the entity may revert ANY
// revision of it, not just their own — see store.CanEditVariant's
// doc comment for the (deliberately permissive, for now) rule that
// applies to variants.
func (s *Server) handleRevertRevision(w http.ResponseWriter, r *http.Request) {
	revisionID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid revision id")
		return
	}
	person := personFromContext(r)

	entityType, entityID, err := s.store.RevisionEntity(r.Context(), revisionID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	switch entityType {
	case "variant":
		variantID, err := strconv.ParseInt(entityID, 10, 64)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "revision has a non-numeric variant entity_id")
			return
		}
		rights, err := s.store.SongRightsFor(r.Context(), person.ID, variantID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		canEdit := rights.Edit
		// The Big Songbook's song: its staff and trusted editors revert
		// any edit; its author their own while it is the latest.
		if rights.Catalogue {
			if canEdit, err = s.store.MayRevertCatalogue(r.Context(), person.ID, revisionID); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if !canEdit {
			httpError(w, http.StatusForbidden, "no edit rights on this variant")
			return
		}
	case "band_member":
		// Carries a band_id — peek it off the revision being reverted
		// itself, not off the entity's current row (which, for an "add"
		// revision, this very revert may be about to delete).
		bandID, err := s.store.PeekRevisionBandID(r.Context(), revisionID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Undoing a membership change (an add, a removal, a role) is
		// giving roles: manage_roles (a guest reverting their own
		// demotion was a way round the core-only switch).
		may, err := s.bandPrivilege(r, bandID, store.BandPrivManageRoles)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !may {
			httpError(w, http.StatusForbidden, "your role in this band doesn't let you change its members' roles")
			return
		}
	case "setlist":
		setlistID, err := strconv.ParseInt(entityID, 10, 64)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "revision has a non-numeric setlist entity_id")
			return
		}
		canEdit, err := s.store.CanEditSetlistByID(r.Context(), person.ID, setlistID)
		if errors.Is(err, store.ErrNotFound) {
			// The setlist row itself may be gone (this revert is undoing
			// its own creation, or a prior revert already deleted it) —
			// fall back to the band_id carried in the revision's own
			// snapshot, same reasoning as the band_member/
			// band_catalog_entry case above. A person-scoped setlist
			// that's gone this way simply can't be reverted any more
			// (no owner to peek at that doesn't require the row), which
			// is fine: there's nothing left to revert to.
			bandID, peekErr := s.store.PeekRevisionBandID(r.Context(), revisionID)
			if peekErr != nil || bandID == 0 {
				httpError(w, http.StatusNotFound, err.Error())
				return
			}
			may, memErr := s.bandPrivilege(r, bandID, store.BandPrivManageSetlists)
			if memErr != nil {
				httpError(w, http.StatusInternalServerError, memErr.Error())
				return
			}
			if !may {
				httpError(w, http.StatusForbidden, "your role in this band doesn't let you change its set lists")
				return
			}
		} else if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		} else if !canEdit {
			httpError(w, http.StatusForbidden, "no edit rights on this setlist")
			return
		}
	default:
		httpError(w, http.StatusBadRequest, "revert not supported for entity_type "+entityType)
		return
	}

	if err := s.store.RevertRevision(r.Context(), revisionID, person.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reverted": true})
}

// handleUpdateVariant is PUT /api/variants/{id} — edits a variant's
// content directly by its row id, the path personal- and band-owned
// variants are edited through (see store.SaveVariantByID's doc comment
// for why this is a separate path from PUT /api/songs/{book}/{stem}:
// that one is gated on project-editor role, which a fork's owner or a
// band member need not hold on the project the variant happens to live
// under).
func (s *Server) handleUpdateVariant(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	person := personFromContext(r)
	rights, err := s.store.SongRightsFor(r.Context(), person.ID, variantID)
	actorPersonID := person.ID
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !rights.Edit && !rights.Contribute {
		httpError(w, http.StatusForbidden, "no edit rights on this variant")
		return
	}

	var req struct {
		Title         string `json:"title"`
		Artist        string `json:"artist"`
		ReferenceURL  string `json:"referenceUrl"`
		Body          string `json:"body"`
		ExpectVersion string `json:"expectVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Saved over a version the song moved on from: merged with what
	// changed since (merge_at_save.go).
	merged, expect, ok := s.mergeAtSave(w, r, variantID, req.Body, req.ExpectVersion)
	if !ok {
		return
	}
	req.Body, req.ExpectVersion = merged, expect
	// The Big Songbook's song: weighed against its recording first.
	if s.catalogueSave(w, r, variantID, req.Title, req.Artist, req.ReferenceURL, req.Body, req.ExpectVersion) {
		return
	}
	newVersion, err := s.store.SaveVariantByID(r.Context(), variantID, req.Title, req.Artist, req.ReferenceURL, req.Body, req.ExpectVersion, actorPersonID)
	if errors.Is(err, store.ErrConflict) {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": newVersion})
}

// handlePutVariantGenre sets the free-text genre on variantID's parent
// WORK (see migration 0030 — genre is a property of the song, not of one
// person's/band's particular arrangement of it), via Prep's compact
// header-fields row alongside time signature/tempo. A standalone
// endpoint rather than folded into handleUpdateVariant's body/title/
// artist/reference save, since genre lives on a different table and
// isn't part of the raw .song-format grammar.
func (s *Server) handlePutVariantGenre(w http.ResponseWriter, r *http.Request) {
	variantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid variant id")
		return
	}
	person := personFromContext(r)
	canEdit, err := s.store.CanEditVariant(r.Context(), person.ID, variantID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !canEdit {
		httpError(w, http.StatusForbidden, "no edit rights on this variant")
		return
	}
	v, err := s.store.GetVariant(r.Context(), variantID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var req struct {
		Genre string `json:"genre"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetWorkGenre(r.Context(), v.WorkID, strings.TrimSpace(req.Genre)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"genre": req.Genre})
}

// historyEdit is one row of a song's History: the edit recorded by a
// revision (its snapshot is the song before the edit; after it comes the
// next revision's snapshot, or the song as it is now), described in the
// song's own terms by internal/songdiff.
type historyEdit struct {
	Changes []songdiff.Change
	More    int // changes left out of Changes
	Lines   []songdiff.Line
}

const historyMaxChanges = 8

func (s *Server) describeEdits(variantID int64, revisions []store.Revision) map[int64]historyEdit {
	out := map[int64]historyEdit{}
	after, _, err := s.store.LoadVariantRaw(variantID)
	if err != nil {
		return out
	}
	for _, rv := range revisions { // newest first
		var snap store.VariantSnapshot
		if json.Unmarshal(rv.Snapshot, &snap) != nil {
			continue
		}
		changes := songdiff.Describe(snap.Body, after)
		e := historyEdit{Changes: changes, Lines: songdiff.Lines(snap.Body, after)}
		if len(changes) > historyMaxChanges {
			e.Changes, e.More = changes[:historyMaxChanges], len(changes)-historyMaxChanges
		}
		out[rv.ID] = e
		after = snap.Body
	}
	return out
}
