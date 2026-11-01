package httpapi

import (
	"fmt"
	"net/http"

	"app/internal/pdf"
)

// handleExportSongPDF renders one song to a one-page (best-effort) PDF via
// the v2/ Typst pipeline. Requires s.pdf to be configured (--v2-root set);
// otherwise it reports 503 rather than silently pretending to succeed.
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
	data, err := s.pdf.ExportSong(r.Context(), book, body, pdf.Options{})
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.pdf"`, stem))
	w.Write(data)
}

// handleExportSetlistPDF renders every entry in a setlist to one combined
// PDF. Setlist entries can reference songs from OTHER projects than the
// setlist's own (see internal/setlist's Book/Stem-per-entry model); an
// entry whose project the requester has no role on at all is skipped
// (noted via X-Songbooks-Skipped response headers) rather than failing the
// whole export over one inaccessible song.
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

	var refs []pdf.SongRef
	roleCache := map[string]bool{}
	accessible := func(project string) bool {
		if ok, cached := roleCache[project]; cached {
			return ok
		}
		_, ok, err := s.store.GetRole(r.Context(), person.ID, project)
		ok = ok && err == nil
		roleCache[project] = ok
		return ok
	}
	var skipped int
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

	title := l.Get("title")
	if title == "" {
		title = stem
	}
	data, err := s.pdf.ExportSetlist(r.Context(), title, refs, pdf.Options{})
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	if skipped > 0 {
		w.Header().Set("X-Songbooks-Skipped-Entries", fmt.Sprintf("%d", skipped))
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.pdf"`, stem))
	w.Write(data)
}
