package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"app/internal/lilypond"
	"app/internal/song"
	"app/internal/store"
)

// This file is the second, independent PDF export route family — real
// engraved chords-over-a-staff notation plus sung lyrics via LilyPond,
// alongside (not instead of) the existing Typst "chart" export in
// handlers_pdf.go. See internal/lilypond's package doc for what this
// pipeline does and doesn't attempt (no melodic transcription).
//
// Unlike the Typst bridge, s.ly needs no external template tree to be
// configured, so it's always non-nil — a missing `lilypond` binary on
// PATH only surfaces as a 500 at request time (same as any other shell-
// out failure), not as a 503 "not configured" the way a missing
// --v2-root does for the Typst routes.

// handleExportSongLilyPDF renders one song to a LilyPond-engraved PDF.
func (s *Server) handleExportSongLilyPDF(w http.ResponseWriter, r *http.Request) {
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
	data, err := s.ly.ExportSong(r.Context(), song.Parse(body))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering LilyPond PDF: "+err.Error())
		return
	}
	writePDF(w, strconv.FormatInt(id, 10), data)
}

// handleExportBandRepertoireLilyPDF renders a whole band's catalog (every
// song pointed to from band_catalog_entries, see store.ListBandCatalog)
// to one combined LilyPond-engraved PDF — the LilyPond pipeline's "band
// repertoire" export, a new capability this phase adds (the Typst
// pipeline never had one; only single-song and setlist export existed
// before). Entries are grouped by nothing in particular (Group: "")
// since a band's repertoire, unlike a setlist, has no Set structure —
// one page per song, book-sorted by ListBandCatalog's own alphabetical
// order.
func (s *Server) handleExportBandRepertoireLilyPDF(w http.ResponseWriter, r *http.Request) {
	bandID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid band id")
		return
	}
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	catalog, err := s.store.ListBandCatalog(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	entries, skipped := loadBandRepertoireSongs(r.Context(), s.store, catalog)
	data, err := s.ly.ExportBook(r.Context(), band.Name, entries)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering LilyPond PDF: "+err.Error())
		return
	}
	if skipped > 0 {
		w.Header().Set("X-Songbooks-Skipped-Entries", fmt.Sprintf("%d", skipped))
	}
	writePDF(w, "repertoire", data)
}

func loadBandRepertoireSongs(ctx context.Context, st *store.Store, catalog []store.BandCatalogEntry) (entries []lilypond.BookEntry, skipped int) {
	for _, e := range catalog {
		body, _, err := st.LoadVariantRaw(e.VariantID)
		if err != nil {
			skipped++
			continue
		}
		entries = append(entries, lilypond.BookEntry{Song: song.Parse(body)})
	}
	return entries, skipped
}

func writePDF(w http.ResponseWriter, filenameStem string, data []byte) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.pdf"`, filenameStem))
	w.Write(data)
}
