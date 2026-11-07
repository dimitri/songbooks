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

// setlistOptionsFromRequest is optionsFromRequest for a gig/setlist
// export: single-column by default, since it's printed to read off a
// music stand from a few feet away, where the normal two-column chart
// isn't legible — ?cols=2 opts back into the dense layout if wanted.
func setlistOptionsFromRequest(r *http.Request) pdf.Options {
	o := optionsFromRequest(r)
	o.Columns = 1
	if c, err := strconv.Atoi(r.URL.Query().Get("cols")); err == nil && c > 0 {
		o.Columns = c
	}
	return o
}

// handleExportSongPDF renders one song to a one-page (best-effort) PDF.
func (s *Server) handleExportSongPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	body, _, err := s.store.LoadVariantRaw(id)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such song")
		return
	}
	data, err := s.pdf.ExportSong(r.Context(), "", body, optionsFromRequest(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	writePDF(w, strconv.FormatInt(id, 10), data)
}

// resolveByIDSetlistRefs resolves a setlist's variant entries to
// pdf.SongRef, skipping (and counting) any that fail to load.
func resolveByIDSetlistRefs(ctx context.Context, s *Server, l *setlist.Setlist) (refs []pdf.SongRef, skipped int) {
	for _, set := range l.Sets {
		for _, e := range set.Entries {
			if !e.IsVariant() {
				skipped++
				continue
			}
			body, _, err := s.store.LoadVariantRaw(e.VariantID)
			if err != nil {
				skipped++
				continue
			}
			refs = append(refs, pdf.SongRef{Group: set.Label, Stem: strconv.FormatInt(e.VariantID, 10), Body: body, Note: e.Note})
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

// setlistFrontMatter resolves event metadata for an id-scoped setlist: the
// band's own name (band-scoped setlists only) plus its soonest-upcoming
// gig's date/venue, if it has one. A setlist with no gigs gets an empty
// EventTitle, which ExportSetlist renders as a plain title-plate rather
// than a half-filled event page.
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

// handleExportSetlistByIDPDF renders a person/band-scoped setlist to one
// combined PDF — cover, title/event page, contents, then every song.
func (s *Server) handleExportSetlistByIDPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server")
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

	data, err := s.pdf.ExportSetlist(r.Context(), title, front, refs, setlistOptionsFromRequest(r))
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
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server")
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

// handleExportSongbookPDF renders a songbook's entire song list to one
// combined PDF (cover, plain title page, contents, every song) — in the
// songbook's own canonical song_order, the same ordering the songbook's
// own page already shows.
func (s *Server) handleExportSongbookPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	person := personFromContext(r)
	if ok, err := s.store.CanAccessSongbook(r.Context(), person.ID, id); err != nil || !ok {
		http.NotFound(w, r)
		return
	}
	sb, err := s.store.GetSongbook(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	groups, err := s.store.ListSongsOrdered(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such songbook")
		return
	}

	var refs []pdf.SongRef
	for _, g := range groups {
		groupLabel := g.Label
		if groupLabel == "Songs" {
			groupLabel = ""
		}
		for _, vid := range g.VariantIDs {
			body, _, err := s.store.LoadVariantRaw(vid)
			if err != nil {
				continue
			}
			refs = append(refs, pdf.SongRef{Group: groupLabel, Stem: strconv.FormatInt(vid, 10), Body: body})
		}
	}
	if len(refs) == 0 {
		httpError(w, http.StatusNotFound, "no songs in this songbook")
		return
	}

	data, err := s.pdf.ExportSongbook(r.Context(), sb.Name, refs, optionsFromRequest(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	writePDF(w, sb.Name+"-songbook", data)
}

// handleExportBandSongbookPDF is handleExportSongbookPDF's sibling for a
// band's own default songbook (store.ListBandCatalog) — alphabetical by
// title; no Set structure (a repertoire isn't divided into performance
// sets the way a setlist is).
func (s *Server) handleExportBandSongbookPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server")
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
		body, _, err := s.store.LoadVariantRaw(e.VariantID)
		if err != nil {
			continue
		}
		refs = append(refs, pdf.SongRef{Stem: strconv.FormatInt(e.VariantID, 10), Body: body})
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

// handleExportCommunitySongbookPDF is handleExportBandSongbookPDF's sibling
// for a community's own default songbook (store.ListCommunityCatalog).
func (s *Server) handleExportCommunitySongbookPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server")
		return
	}
	communityID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid community id")
		return
	}
	community, err := s.store.GetCommunity(r.Context(), communityID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	catalog, err := s.store.ListCommunityCatalog(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var refs []pdf.SongRef
	for _, e := range catalog {
		body, _, err := s.store.LoadVariantRaw(e.VariantID)
		if err != nil {
			continue
		}
		refs = append(refs, pdf.SongRef{Stem: strconv.FormatInt(e.VariantID, 10), Body: body})
	}
	if len(refs) == 0 {
		httpError(w, http.StatusNotFound, "this community's repertoire has no songs yet")
		return
	}

	data, err := s.pdf.ExportSongbook(r.Context(), community.Name, refs, optionsFromRequest(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	writePDF(w, community.Name+"-songbook", data)
}
