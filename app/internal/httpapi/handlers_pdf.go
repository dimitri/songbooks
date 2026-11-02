package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"app/internal/pdf"
	"app/internal/setlist"
	"app/internal/store"
)

// optionsFromRequest reads the view-mode/notes choices a PDF export link
// can carry — ?mode=jazz|map|lyrics (anything else, including absent,
// falls back to pdf.ModeDefault — today's only behavior, so an existing
// bookmarked/linked export.pdf URL with no query params is unaffected)
// and ?notes=0 (the only way to turn OFF notes — HideNotes's zero value
// is false, matching pdfnative's existing unconditional rendering of
// Section.Notes, so an absent ?notes= param also reproduces today's
// behavior exactly).
func optionsFromRequest(r *http.Request) pdf.Options {
	return pdf.Options{
		Mode:      pdf.ModeFromString(r.URL.Query().Get("mode")),
		HideNotes: r.URL.Query().Get("notes") == "0",
	}
}

// handleExportSongPDF renders one song to a one-page (best-effort) PDF.
// Requires s.pdf to be configured (--v2-root set, for the legacy Typst
// Bridge path); otherwise it reports 503 rather than silently pretending
// to succeed.
func (s *Server) handleExportSongPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server (no --v2-root)")
		return
	}
	book, stem := r.PathValue("book"), r.PathValue("stem")
	body, _, err := s.store.LoadRaw(book, stem)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such song")
		return
	}
	data, err := s.pdf.ExportSong(r.Context(), book, body, optionsFromRequest(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	writePDF(w, stem, data)
}

// resolveLegacySetlistRefs resolves every entry of a legacy project-scoped
// (book/stem) setlist to a pdf.SongRef, skipping (and counting) any whose
// project the requester has no role on, or that fail to load.
func resolveLegacySetlistRefs(ctx context.Context, s *Server, personID int64, l *setlist.Setlist) (refs []pdf.SongRef, skipped int) {
	roleCache := map[string]bool{}
	accessible := func(project string) bool {
		if ok, cached := roleCache[project]; cached {
			return ok
		}
		_, ok, err := s.store.GetRole(ctx, personID, project)
		ok = ok && err == nil
		roleCache[project] = ok
		return ok
	}
	for _, set := range l.Sets {
		for _, e := range set.Entries {
			if !accessible(e.Book) {
				skipped++
				continue
			}
			body, _, err := s.store.LoadRaw(e.Book, e.Stem)
			if err != nil {
				skipped++
				continue
			}
			refs = append(refs, pdf.SongRef{Group: set.Label, Book: e.Book, Stem: e.Stem, Body: body})
		}
	}
	return refs, skipped
}

// resolveByIDSetlistRefs is resolveLegacySetlistRefs' sibling for the
// id-addressed (person/band/project scope) setlist model, whose entries
// may reference a variant directly (setlist.Entry.IsVariant()) instead of
// a book/stem pair — resolved to book/stem via GetVariant the same way
// handleSetlistClicktrack already does, since every variant still belongs
// to a project (see store.CatalogVariant's own doc comment).
func resolveByIDSetlistRefs(ctx context.Context, s *Server, l *setlist.Setlist) (refs []pdf.SongRef, skipped int) {
	for _, set := range l.Sets {
		for _, e := range set.Entries {
			book, stem := e.Book, e.Stem
			if e.IsVariant() {
				v, err := s.store.GetVariant(ctx, e.VariantID)
				if err != nil {
					skipped++
					continue
				}
				book, stem = v.Book, v.Stem
			}
			if book == "" || stem == "" {
				skipped++
				continue
			}
			body, _, err := s.store.LoadRaw(book, stem)
			if err != nil {
				skipped++
				continue
			}
			refs = append(refs, pdf.SongRef{Group: set.Label, Book: book, Stem: stem, Body: body})
		}
	}
	return refs, skipped
}

func gigSubtitle(date, venue string) string {
	switch {
	case date != "" && venue != "":
		return date + " · " + venue
	case date != "":
		return date
	case venue != "":
		return venue
	default:
		return ""
	}
}

// handleExportSetlistPDF renders every entry in a legacy project-scoped
// (book/stem) setlist to one combined PDF — cover, title/event page (the
// legacy .setlist format's own date/venue meta fields feed the event
// title page when set), contents, then every song. Entries can reference
// songs from OTHER projects than the setlist's own; an entry whose
// project the requester has no role on at all is skipped (noted via
// X-Songbooks-Skipped response headers) rather than failing the whole
// export over one inaccessible song.
func (s *Server) handleExportSetlistPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server (no --v2-root)")
		return
	}
	book, stem := r.PathValue("book"), r.PathValue("stem")
	l, _, err := s.store.LoadSetlist(book, stem)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such setlist")
		return
	}
	person := personFromContext(r)

	refs, skipped := resolveLegacySetlistRefs(r.Context(), s, person.ID, l)
	title := orDefault(l.Get("title"), stem)
	front := pdf.FrontMatter{EventTitle: title, Date: l.Get("date"), Venue: l.Get("venue")}
	if front.Date == "" && front.Venue == "" {
		front.EventTitle = ""
	}

	data, err := s.pdf.ExportSetlist(r.Context(), title, front, refs, optionsFromRequest(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	if skipped > 0 {
		w.Header().Set("X-Songbooks-Skipped-Entries", fmt.Sprintf("%d", skipped))
	}
	writePDF(w, stem, data)
}

// handleExportSetlistPageOnlyPDF renders just the running-order table for
// a legacy project-scoped setlist — a single page, no song pages. For a
// music stand, not the full book.
func (s *Server) handleExportSetlistPageOnlyPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server (no --v2-root)")
		return
	}
	book, stem := r.PathValue("book"), r.PathValue("stem")
	l, _, err := s.store.LoadSetlist(book, stem)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such setlist")
		return
	}
	person := personFromContext(r)
	refs, _ := resolveLegacySetlistRefs(r.Context(), s, person.ID, l)
	title := orDefault(l.Get("title"), stem)
	data, err := s.pdf.ExportSetlistPageOnly(r.Context(), title, gigSubtitle(l.Get("date"), l.Get("venue")), refs, optionsFromRequest(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	writePDF(w, stem+"-setlist", data)
}

// setlistFrontMatter resolves event metadata for an id-scoped setlist: the
// band's own name (band-scoped setlists only) plus its soonest-upcoming
// gig's date/venue, if it has one. A setlist with no gigs gets an empty
// EventTitle, which ExportSetlist/render-book.typ render as a plain
// title-plate rather than a half-filled event page.
func (s *Server) setlistFrontMatter(r *http.Request, meta *store.SetlistMeta, title string) pdf.FrontMatter {
	front := pdf.FrontMatter{EventTitle: title}
	if meta.OwnerBandID != nil {
		if band, err := s.store.GetBand(r.Context(), *meta.OwnerBandID); err == nil {
			front.BandName = band.Name
		}
	}
	if gigs, err := s.store.ListSetlistGigs(r.Context(), meta.ID); err == nil && len(gigs) > 0 {
		g := gigs[0]
		if g.Date != nil {
			front.Date = g.Date.Format("2006-01-02")
		}
		front.Venue = g.Venue
	}
	if front.Date == "" && front.Venue == "" {
		front.EventTitle = ""
	}
	return front
}

// handleExportSetlistByIDPDF renders a person/band/project-scoped setlist
// (addressed by id, the "Phase 3" model) to one combined PDF — same
// cover/title/contents/songs structure as the legacy handler above.
func (s *Server) handleExportSetlistByIDPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server (no --v2-root)")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid setlist id")
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

	title := orDefault(meta.Title, "setlist")
	refs, skipped := resolveByIDSetlistRefs(r.Context(), s, l)
	front := s.setlistFrontMatter(r, meta, title)

	data, err := s.pdf.ExportSetlist(r.Context(), title, front, refs, optionsFromRequest(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	if skipped > 0 {
		w.Header().Set("X-Songbooks-Skipped-Entries", fmt.Sprintf("%d", skipped))
	}
	writePDF(w, title, data)
}

// handleExportSetlistPageOnlyByIDPDF is the id-addressed setlist's
// single-page (no song pages) export — same data resolution as
// handleExportSetlistByIDPDF.
func (s *Server) handleExportSetlistPageOnlyByIDPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server (no --v2-root)")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid setlist id")
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

	title := orDefault(meta.Title, "setlist")
	refs, _ := resolveByIDSetlistRefs(r.Context(), s, l)
	front := s.setlistFrontMatter(r, meta, title)
	data, err := s.pdf.ExportSetlistPageOnly(r.Context(), title, gigSubtitle(front.Date, front.Venue), refs, optionsFromRequest(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	writePDF(w, title+"-setlist", data)
}

// handleExportProjectSongbookPDF renders a project/book's entire song
// list to one combined PDF (cover, plain title page, contents, every
// song) — in the project's own canonical song_order (store.ListSongsOrdered),
// the same ordering the book's own page already shows. A project with
// real "# SET:"-style groups in its order (an album-sectioned book, not
// just the single "Songs" fallback) carries those groups through to the
// contents table too — richer structure the book's own ordering already
// has, not invented for this export.
func (s *Server) handleExportProjectSongbookPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server (no --v2-root)")
		return
	}
	book := r.PathValue("book")
	groups, err := s.store.ListSongsOrdered(book)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such project")
		return
	}

	var refs []pdf.SongRef
	for _, g := range groups {
		groupLabel := g.Label
		if groupLabel == "Songs" {
			groupLabel = ""
		}
		for _, stem := range g.Stems {
			body, _, err := s.store.LoadRaw(book, stem)
			if err != nil {
				continue
			}
			refs = append(refs, pdf.SongRef{Group: groupLabel, Book: book, Stem: stem, Body: body})
		}
	}
	if len(refs) == 0 {
		httpError(w, http.StatusNotFound, "no songs in this project")
		return
	}

	data, err := s.pdf.ExportSongbook(r.Context(), book, refs, optionsFromRequest(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	writePDF(w, book+"-songbook", data)
}

// handleExportBandSongbookPDF is handleExportProjectSongbookPDF's sibling
// for a band's repertoire (store.ListBandCatalog) — most-recently-added-
// first order, same as that order already is throughout the rest of the
// app; no Set structure (a repertoire isn't divided into performance sets
// the way a setlist is).
func (s *Server) handleExportBandSongbookPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server (no --v2-root)")
		return
	}
	bandID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid band id")
		return
	}
	band, err := s.store.GetBand(r.Context(), bandID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	catalog, err := s.store.ListBandCatalog(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var refs []pdf.SongRef
	for _, e := range catalog {
		body, _, err := s.store.LoadRaw(e.Book, e.Stem)
		if err != nil {
			continue
		}
		refs = append(refs, pdf.SongRef{Book: e.Book, Stem: e.Stem, Body: body})
	}
	if len(refs) == 0 {
		httpError(w, http.StatusNotFound, "this band's repertoire has no songs yet")
		return
	}

	data, err := s.pdf.ExportSongbook(r.Context(), band.Name, refs, optionsFromRequest(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	writePDF(w, band.Name+"-songbook", data)
}
