package pdfnative

import (
	"context"

	"app/internal/pdf"
	"app/internal/song"

	"github.com/signintech/gopdf"
)

// Renderer is a pure-Go, in-process implementation of pdf.Exporter — see
// the package doc comment in fonts.go for why this exists. It shells out
// to nothing: New takes no arguments (no v2Root, no external binary to
// find) because everything it needs (fonts, layout rules) is compiled in.
type Renderer struct{}

// New constructs a Renderer. There is nothing to configure — unlike
// pdf.New(v2Root), which needed a path to the Typst pipeline, this type
// has no external dependency at all.
func New() *Renderer { return &Renderer{} }

var _ pdf.Exporter = (*Renderer)(nil)

func newDoc() (*gopdf.GoPdf, error) {
	doc := &gopdf.GoPdf{}
	doc.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageW, H: pageH}})
	if err := registerFonts(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// ExportSong renders one song's body text to a one-page (best-effort) PDF.
func (r *Renderer) ExportSong(ctx context.Context, book, body string, opts pdf.Options) ([]byte, error) {
	s := song.Parse(body)
	doc, err := newDoc()
	if err != nil {
		return nil, err
	}
	if err := renderSong(doc, s, themeFor(opts.Theme), opts.Mode, opts.HideNotes, opts.Cols()); err != nil {
		return nil, err
	}
	return pdfBytes(doc)
}

// songPages returns how many pages s will actually take (1, almost
// always; 2 only for a chart that overflows even at the smallest
// candidate size; always 1 for ModeMap, which never measures/fits — it's
// a short fixed-format list, not a chart that can overflow in practice,
// and giving it the same treatment as the other modes would require
// duplicating renderSongMap's layout into the measurement path for no
// real benefit) — used to number the contents table's PAGE column without
// a full two-pass render of the songs themselves. measureDoc must already
// be Start()ed with fonts registered (gopdf's MeasureTextWidth needs a
// loaded font even in a draw=false pass) — reused across every song in a
// songbook rather than constructed fresh per call.
func songPages(measureDoc *gopdf.GoPdf, s *song.Song, th theme, mode pdf.Mode, hideNotes bool, cols int) int {
	if mode == pdf.ModeMap {
		return 1
	}
	size, allowPage2 := fitSongToPage(measureDoc, s, th, mode, hideNotes, cols)
	if !allowPage2 {
		return 1
	}
	c := newCanvas(measureDoc, false, th, cols, true)
	renderSongBody(c, s, size, mode, hideNotes)
	return c.pages
}

// buildEntries parses every SongRef once (shared by the song-page pass and
// the contents table) and resolves each one's page number deterministically:
// cover=1, title=2, contents=3, songs start at 4 — true as long as the
// contents table itself is always exactly one page, which
// renderContentsPage's row-shrinking guarantees (see its own doc comment).
func buildEntries(refs []pdf.SongRef, th theme, withPages bool, mode pdf.Mode, hideNotes bool, cols int) ([]*song.Song, []contentsEntry) {
	parsed := make([]*song.Song, len(refs))
	entries := make([]contentsEntry, len(refs))
	page := 4

	var measureDoc *gopdf.GoPdf
	if withPages && len(refs) > 0 {
		measureDoc = &gopdf.GoPdf{}
		measureDoc.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageW, H: pageH}})
		registerFonts(measureDoc)
	}

	for i, ref := range refs {
		s := song.Parse(ref.Body)
		parsed[i] = s
		e := contentsEntry{
			Title:  s.Get("title"),
			Artist: s.Get("artist"),
			Key:    s.Get("key"),
			Tempo:  s.Get("tempo"),
			Group:  ref.Group,
		}
		if e.Title == "" {
			e.Title = ref.Stem
		}
		if withPages {
			e.Page = page
			page += songPages(measureDoc, s, th, mode, hideNotes, cols)
		}
		entries[i] = e
	}
	return parsed, entries
}

// ExportSetlist renders a cover page, a title page (event-aware when front
// carries real date/venue/event-title, plain otherwise), a contents/
// setlist page grouped by Set, then one page per song in order.
func (r *Renderer) ExportSetlist(ctx context.Context, title string, front pdf.FrontMatter, refs []pdf.SongRef, opts pdf.Options) ([]byte, error) {
	th := themeFor(opts.Theme)
	doc, err := newDoc()
	if err != nil {
		return nil, err
	}

	coverTitle := front.CoverTitle
	if coverTitle == "" {
		coverTitle = title
	}
	renderCover(doc, coverTitle)

	// Same selection rule as render-book.typ: an event title page only
	// when there's real event metadata to show, else the plain title
	// plate (a songbook export, or a setlist with no gig attached).
	if front.EventTitle != "" || front.Date != "" || front.Venue != "" {
		eventTitle := front.EventTitle
		if eventTitle == "" {
			eventTitle = title
		}
		renderEventTitlePlate(doc, eventTitle, front.BandName, front.Date, front.Venue)
	} else {
		renderTitlePlate(doc, title, "")
	}

	parsed, entries := buildEntries(refs, th, true, opts.Mode, opts.HideNotes, opts.Cols())
	renderContentsPage(doc, title, "", entries, true, th)

	for _, s := range parsed {
		if err := renderSong(doc, s, th, opts.Mode, opts.HideNotes, opts.Cols()); err != nil {
			return nil, err
		}
	}
	return pdfBytes(doc)
}

// ExportSongbook renders an entire songbook (a project's full song list, or
// a band's full repertoire) — the same cover/title/contents/songs
// structure as ExportSetlist, with no Set grouping and no event metadata
// (a songbook isn't tied to one gig).
func (r *Renderer) ExportSongbook(ctx context.Context, title string, refs []pdf.SongRef, opts pdf.Options) ([]byte, error) {
	return r.ExportSetlist(ctx, title, pdf.FrontMatter{}, refs, opts)
}

// ExportSetlistPageOnly renders just the running-order table (no PAGE
// column, since no song pages follow it) as a single-page PDF — cheap: it
// never fits/lays out a single song's chart.
func (r *Renderer) ExportSetlistPageOnly(ctx context.Context, title, subtitle string, refs []pdf.SongRef, opts pdf.Options) ([]byte, error) {
	th := themeFor(opts.Theme)
	doc, err := newDoc()
	if err != nil {
		return nil, err
	}
	// mode/hideNotes are irrelevant here (withPages=false means songPages,
	// the only thing that reads them, is never called) — passed through
	// anyway only to satisfy buildEntries' signature.
	_, entries := buildEntries(refs, th, false, opts.Mode, opts.HideNotes, opts.Cols())
	// growToFill=true: this is the standalone "music stand / stage floor"
	// export (no song pages follow, nothing else shares this page) — a
	// short setlist should use the spare page space for bigger, more
	// legible rows, not leave it blank the way the multi-page contents
	// table's own sizing (shared via the plain renderContentsPage) does.
	renderContentsPageSized(doc, title, subtitle, entries, false, th, true)
	return pdfBytes(doc)
}

func pdfBytes(doc *gopdf.GoPdf) ([]byte, error) {
	return doc.GetBytesPdf(), nil
}
