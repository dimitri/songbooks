package pdfnative

// The Jazz chart on A4: the very chart the song page draws in its Jazz
// view (internal/httpapi/jazzchart.go builds it, template "jazz-chart" in
// play.html and static/css/jazz.css draw it on screen), drawn here from
// the same lines and cells — the form, the barlines, the signs, the
// chords on their beat cells are decided once, by the web's builder, and
// handed over as a JazzChart; nothing here lays a chart out of a song on
// its own.
//
// The drawing follows jazz.css rule for rule, in the same em units: one
// font size, the page's content width / 21.5, every offset and size a
// fraction of it (the CSS rule each measure comes from is named beside
// it). The page is the chart's print look (jazz.css's @media print):
// white paper, black ink, no player highlights. The whole page width is
// used; a form too long for the page is set a little smaller to stay on
// one, or runs on over more pages, cut at a section when it can, never
// leaving one line alone on the last.

import (
	"strings"

	"app/internal/song"

	"github.com/signintech/gopdf"
)

// JazzChart is a song's Jazz chart as the web builds it (httpapi's
// jazzChart), with the strings its page translates already translated.
type JazzChart struct {
	Title, Style, Composer string
	Tempo                  string
	Compound               bool // the tempo counts dotted quarters: "♩. = 72"
	// The key under the composer: its root drawn with a music-font
	// accidental, then the rest ("m"); KeyRoot empty: Key as written.
	KeyRoot JazzNote
	Key     string
	// The page's words, translated: "Key", "with repeats", "lead".
	KeyWord, WithRepeatsWord, LeadWord string
	Lines                              []JazzLine
}

// JazzLine is one system: 4 bars (and the song's lead-in cells, narrow,
// ahead of them on the first line).
type JazzLine struct {
	Lead  int  // narrow lead-in cells it opens with
	Gap   bool // a printed section's first line: room above it
	Coda  bool // the coda's first line: more room above it
	Cells []JazzCell
	Notes []string // the section's "note:" directions, under the line
}

// JazzCell is one printed bar (httpapi's jzCellView, without the
// player's data).
type JazzCell struct {
	Pad bool // an empty column (a later ending aligned under the first)

	Left, Right string // barlines: single, sec-start, sec-end, final, rep-start, rep-end, rep-both

	MarkParts []string // the rehearsal mark's box ("In", "V"), wrapping after each "/"
	Name      string   // the section's name, small, beside the box

	TSNum, TSDen string // a time signature before the bar
	TSMid        bool   // inside the line, after the barline

	Volta       string // an ending's label ("1, 2.")
	VoltaSpan   int    // bars its bracket covers on this line
	VoltaClosed bool
	VoltaCont   bool

	Segno, Coda, ToCoda bool
	Texts               []JazzText

	Chords []JazzChord
	Simile int  // 1: "%", 2: "%" with a small 2
	Pair2  bool // the two-bar repeat sign over this bar's left barline
	Rest   bool
	// Lead: one of the song's "lead:" bars, drawn lighter; LeadTag on
	// the first, the small "lead" in its corner.
	Lead, LeadTag bool
}

// JazzText is a word under a bar: "3x", "Fine", "D.S. al Coda".
type JazzText struct {
	S           string
	WithRepeats bool
}

// JazzChord is a chord on its beat cell: X the fraction of the bar it
// starts at, Pad the room before it in em. Text is a chord without a
// root (N.C., a word), drawn as written; else the root, the quality's
// runs, the slash bass.
type JazzChord struct {
	X, Pad     float64
	Text       string
	Root, Bass JazzNote
	Quality    []JazzRun
}

// JazzNote is a note name: its letter, and "b" or "#".
type JazzNote struct{ Letter, Acc string }

// JazzRun is a piece of a chord's quality: text in the hand font, or a
// music-font sign.
type JazzRun struct {
	S    string
	Kind JazzRunKind
}

// JazzRunKind says which font and size a quality run is drawn in
// (jazz.css: .jz-q text, .jz-g, .jz-g.jz-min, .jz-acc2).
type JazzRunKind int

const (
	JazzRunText  JazzRunKind = iota // "7", "sus4": the hand font
	JazzRunSign                     // ø ° + △: the music font
	JazzRunMinor                    // the minor sign, a little bigger
	JazzRunAcc                      // ♭/♯ inside an extension ("7♭9")
)

// SMuFL code points in ChartSymbols (the web's jzGlyph* and the
// template's entities).
const (
	smuflFlat      = ""
	smuflSharp     = ""
	smuflQuarter   = ""
	smuflAugDot    = ""
	smuflSegno     = ""
	smuflCoda      = ""
	smuflWholeRest = ""
	smuflSimile1   = ""
	smuflSimile2   = ""
)

// The print colours (jazz.css @media print): black ink on white, the
// soft ink for names and notes, a lead-in bar's content at half
// opacity.
var (
	jzInk   = rgb{0, 0, 0}
	jzSoft  = rgb{0x33, 0x33, 0x33}
	jzFaded = rgb{0x80, 0x80, 0x80}
	jzPaper = rgb{255, 255, 255}
)

// The page: A4, a margin all round, the chart as wide as the page lets
// it be (.jz-chart's font-size is its width / 21.5).
const (
	jzMargin    = 36.0
	jzEmPerPage = 21.5
	// A form a little longer than a page is set smaller to stay on one,
	// down to this fraction of the full size, before it runs on over
	// two pages.
	jzMinShrink = 0.75
)

// jazzChartOf is ref's Jazz chart, or its song's header alone.
func jazzChartOf(ref SongRef, s *song.Song) *JazzChart {
	if ref.Jazz != nil {
		return ref.Jazz
	}
	return jazzHeaderOnly(s)
}

// jazzHeaderOnly is a chart of s's header and no bars: what a ModeJazz
// export prints of a song nobody built a chart for (the chart is the
// web's, built in internal/httpapi, which passes it in SongRef.Jazz or
// to ExportJazzChart) — as the Jazz view shows no grid for a song
// without one.
func jazzHeaderOnly(s *song.Song) *JazzChart {
	return &JazzChart{
		Title:    s.Get("title"),
		Style:    s.Get("style"),
		Composer: s.Get("artist"),
		Tempo:    strings.TrimSpace(s.Get("tempo")),
		Compound: song.CompoundTime(s.Get("time")),
		Key:      strings.TrimSpace(s.Get("key")),
		KeyWord:  "Key",
	}
}

// jzPainter draws a chart at one em: x0 is the chart's left edge.
type jzPainter struct {
	c   *canvas
	E   float64
	x0  float64
	w   float64 // the chart's width (21.5 em)
	sty JazzChart
}

func jzAccGlyph(acc string) string {
	switch acc {
	case "b":
		return smuflFlat
	case "#":
		return smuflSharp
	}
	return ""
}

func (p *jzPainter) mw(s, face string, size float64) float64 {
	if s == "" {
		return 0
	}
	return p.c.measureWidth(s, face, size)
}

func (p *jzPainter) text(x, base float64, s, face string, size float64, col rgb) {
	if s == "" {
		return
	}
	p.c.textAt(x, base, s, face, size, col)
}

// ---- heights (all in em, so the page fits at any size) ----

const (
	jzHeadH    = 1.32 // .jz-title's line, the tallest of the header
	jzHeadGap  = 0.9  // .jz-head margin-bottom
	jzSysH     = 2.3  // .jz-sys height
	jzGapTop   = 0.55 // .jz-sys.gap
	jzCodaTop  = 1.4  // .jz-sys.coda
	jzNoteSize = 0.3  // .jz-notes font-size; its margins are in its own em
	// Room kept above a continued page's first line, for what stands
	// over its top (a rehearsal box, a coda sign, an ending's name).
	jzPageTop = 0.9
)

// notesLines wraps a line's notes at the chart's width (.jz-notes:
// margin-left 1.5em of its .3em font, line-height 1).
func (p *jzPainter) notesLines(notes []string) [][]string {
	size := jzNoteSize * p.E
	maxW := p.w - 1.5*size
	var out [][]string
	space := p.mw(" ", faceSansIt, size)
	for _, n := range notes {
		var line []string
		lw := 0.0
		for _, word := range strings.Fields(n) {
			ww := p.mw(word, faceSansIt, size)
			if len(line) > 0 && lw+space+ww > maxW {
				out = append(out, line)
				line, lw = nil, 0
			}
			if len(line) > 0 {
				lw += space
			}
			line = append(line, word)
			lw += ww
		}
		if len(line) > 0 {
			out = append(out, line)
		}
	}
	return out
}

// lineMetrics is each line's top margin and height (system + notes), in
// em: margins between a line's notes and the next system collapse, as
// the page's block margins do.
func (p *jzPainter) lineMetrics(ch *JazzChart) (tops, heights []float64) {
	prevBottom := jzHeadGap
	for i, ln := range ch.Lines {
		top := 0.0
		switch {
		case ln.Coda:
			top = jzCodaTop
		case ln.Gap:
			top = jzGapTop
		}
		if i == 0 {
			top = max(top, jzHeadGap)
		} else {
			top = max(top, prevBottom)
		}
		h := jzSysH
		prevBottom = 0
		if len(ln.Notes) > 0 {
			n := len(p.notesLines(ln.Notes))
			// .jz-notes { margin: .1em 0 .2em } of its own .3em
			h += 0.1*jzNoteSize + float64(n)*jzNoteSize
			prevBottom = 0.2 * jzNoteSize
		}
		tops = append(tops, top)
		heights = append(heights, h)
	}
	return tops, heights
}

// jzPage is one page's lines, [from, to).
type jzPage struct{ from, to int }

// jazzLayout picks the chart's em, its left edge and how its lines go
// over pages: the page's full width when the chart fits on one page;
// smaller, centred, when it fits at no less than jzMinShrink of that;
// else the full width over several pages.
func jazzLayout(c *canvas, ch *JazzChart) (p *jzPainter, pages []jzPage, tops, heights []float64) {
	fullW := pageW - 2*jzMargin
	availH := pageH - 2*jzMargin
	p = &jzPainter{c: c, E: fullW / jzEmPerPage, x0: jzMargin, w: fullW}
	// Heights are proportional to the em (the notes wrap the same at any
	// size): measured once, in em.
	unit := &jzPainter{c: c, E: 1, x0: 0, w: jzEmPerPage}
	tops, heights = unit.lineMetrics(ch)
	total := jzHeadH
	for i := range tops {
		total += tops[i] + heights[i]
	}
	if total*p.E <= availH || len(ch.Lines) == 0 {
		return p, []jzPage{{0, len(ch.Lines)}}, tops, heights
	}
	if e := availH / total; e >= p.E*jzMinShrink {
		w := e * jzEmPerPage
		p = &jzPainter{c: c, E: e, x0: (pageW - w) / 2, w: w}
		return p, []jzPage{{0, len(ch.Lines)}}, tops, heights
	}
	avail := availH / p.E
	return p, jazzPaginate(tops, heights, avail), tops, heights
}

// jazzPaginate cuts the lines into pages of at most avail em: the first
// page under the header, each next one with jzPageTop of room above its
// first line. A page ends before a section's first line when one is
// near the bottom (no more than 2 lines back), and the last page gets
// at least 2 lines.
func jazzPaginate(tops, heights []float64, avail float64) []jzPage {
	var pages []jzPage
	from := 0
	used := jzHeadH
	for from < len(tops) {
		to := from
		y := used
		for to < len(tops) {
			top := tops[to]
			if to == from && len(pages) > 0 {
				top = jzPageTop
			}
			if to > from && y+top+heights[to] > avail {
				break
			}
			y += top + heights[to]
			to++
		}
		if to < len(tops) {
			// Prefer to end at a section: a gap line among the last 2.
			for k := to; k > from+1 && k >= to-2; k-- {
				if tops[k] >= jzGapTop {
					to = k
					break
				}
			}
		}
		pages = append(pages, jzPage{from, to})
		from = to
		used = 0
	}
	// No orphan: a last page of one line takes the line before it.
	if n := len(pages); n > 1 && pages[n-1].to-pages[n-1].from == 1 && pages[n-2].to-pages[n-2].from > 2 {
		pages[n-2].to--
		pages[n-1].from--
	}
	return pages
}

// jazzPages is how many pages renderJazz will draw ch on (the contents
// table's page numbers).
func jazzPages(doc *gopdf.GoPdf, ch *JazzChart) int {
	c := newCanvas(doc, false, lightTheme, 1, false)
	_, pages, _, _ := jazzLayout(c, ch)
	return len(pages)
}

// renderJazz draws ch from a new page.
func renderJazz(doc *gopdf.GoPdf, ch *JazzChart) {
	c := newCanvas(doc, true, lightTheme, 1, false)
	p, pages, tops, heights := jazzLayout(c, ch)
	p.sty = *ch
	for pi, pg := range pages {
		doc.AddPage()
		y := jzMargin
		if pi == 0 {
			p.head(ch, y)
			y += jzHeadH * p.E
		}
		for li := pg.from; li < pg.to; li++ {
			top := tops[li]
			if li == pg.from && pi > 0 {
				top = jzPageTop
			}
			y += top * p.E
			p.system(ch.Lines[li], y)
			if len(ch.Lines[li].Notes) > 0 {
				p.notes(ch.Lines[li].Notes, y+jzSysH*p.E)
			}
			y += heights[li] * p.E
		}
	}
}

// ---- the header (.jz-head) ----

// head draws the title centred, the style and tempo at the left, the
// composer and key at the right, their bottoms on one line.
func (p *jzPainter) head(ch *JazzChart, top float64) {
	E := p.E
	bottom := top + jzHeadH*E
	sub := 0.78 * 0.5 * E // .jz-sub: .78em of the .5em columns

	// Left: the style, then the tempo (.jz-style).
	leftW := 0.0
	if ch.Tempo != "" {
		// The tempo line: the note (and its dot) in the music font a
		// little lowered, then " = 72" (.jz-bpm).
		bpm := 1.15 * sub
		base := bottom - 0.15*0.5*E - (0.5*bpm + 0.05*bpm)
		x := p.x0
		p.text(x, base+0.05*bpm, smuflQuarter, faceMusic, bpm, jzSoft)
		x += p.mw(smuflQuarter, faceMusic, bpm)
		if ch.Compound {
			x += 0.14 * bpm // .jz-bpm-dot's room from the note
			p.text(x, base+0.05*bpm, smuflAugDot, faceMusic, bpm, jzSoft)
			x += p.mw(smuflAugDot, faceMusic, bpm)
		}
		s := " = " + ch.Tempo
		p.text(x, base, s, faceHand, sub, jzSoft)
		leftW = x + p.mw(s, faceHand, sub) - p.x0
		if ch.Style != "" {
			// The style's line sits above the tempo's (.jz-sub margin-top .25em).
			sbase := base - 0.8*sub - 0.25*sub - 0.2*0.5*E
			p.text(p.x0, sbase, ch.Style, faceHand, 0.5*E, jzInk)
			leftW = max(leftW, p.mw(ch.Style, faceHand, 0.5*E))
		}
	} else if ch.Style != "" {
		p.text(p.x0, bottom-0.15*0.5*E-0.2*0.5*E, ch.Style, faceHand, 0.5*E, jzInk)
		leftW = p.mw(ch.Style, faceHand, 0.5*E)
	}

	// Right: the composer, then "Key: A" (.jz-composer, right-aligned).
	right := p.x0 + p.w
	rightW := 0.0
	keyBase := bottom - 0.15*0.5*E - 0.2*sub
	hasKey := ch.Key != "" || ch.KeyRoot.Letter != ""
	if hasKey {
		label := ch.KeyWord + ": "
		acc := jzAccGlyph(ch.KeyRoot.Acc)
		accSize := 0.9 * sub
		accW := 0.0
		if acc != "" {
			accW = p.mw(acc, faceMusic, accSize) + 2*0.02*accSize
		}
		w := p.mw(label, faceHand, sub) + p.mw(ch.KeyRoot.Letter, faceHand, sub) + accW + p.mw(ch.Key, faceHand, sub)
		x := right - w
		p.text(x, keyBase, label, faceHand, sub, jzSoft)
		x += p.mw(label, faceHand, sub)
		p.text(x, keyBase, ch.KeyRoot.Letter, faceHand, sub, jzSoft)
		x += p.mw(ch.KeyRoot.Letter, faceHand, sub)
		if acc != "" {
			// .jz-key .jz-acc: raised to the letter's top.
			top := keyBase - 0.8*sub - 0.18*accSize
			p.text(x+0.02*accSize, top+0.5*accSize, acc, faceMusic, accSize, jzSoft)
			x += accW
		}
		p.text(x, keyBase, ch.Key, faceHand, sub, jzSoft)
		rightW = w
	}
	if ch.Composer != "" {
		base := bottom - 0.15*0.5*E - 0.2*0.5*E
		if hasKey {
			base = keyBase - 0.8*sub - 0.25*sub - 0.2*0.5*E
		}
		w := p.mw(ch.Composer, faceHand, 0.5*E)
		p.text(right-w, base, ch.Composer, faceHand, 0.5*E, jzInk)
		rightW = max(rightW, w)
	}

	// The title, centred over the page, smaller when it would run into
	// the columns beside it.
	if ch.Title != "" {
		size := jzHeadH * E
		room := p.w - 2*max(leftW, rightW) - 0.8*E
		if w := p.mw(ch.Title, faceHand, size); w > room && room > 0 {
			size = max(size*0.55, size*room/w)
		}
		w := p.mw(ch.Title, faceHand, size)
		p.text(p.x0+(p.w-w)/2, bottom-0.2*size, ch.Title, faceHand, size, jzInk)
	}
}

// ---- a system (.jz-sys) ----

func (p *jzPainter) system(ln JazzLine, top float64) {
	E := p.E
	left := p.x0 + 1.5*E // .jz-sys margin-left: the rehearsal marks' margin
	width := p.w - 1.5*E
	// 4 equal columns; the lead-in's at .45 of one (.jz-sys.has-lead).
	fr := 4 + 0.45*float64(ln.Lead)
	xs := make([]float64, len(ln.Cells))
	ws := make([]float64, len(ln.Cells))
	x := left
	for i := range ln.Cells {
		w := width / fr
		if i < ln.Lead {
			w = 0.45 * width / fr
		}
		xs[i], ws[i] = x, w
		x += w
	}
	for i, cell := range ln.Cells {
		if cell.Pad {
			continue
		}
		p.cell(cell, xs[i], ws[i], top)
	}
	// Signs over the barlines come last, as their z-index has them.
	for i, cell := range ln.Cells {
		if cell.Pad {
			continue
		}
		if cell.Pair2 {
			p.simile(xs[i], top, smuflSimile2, 0, jzInk)
		}
		if len(cell.MarkParts) > 0 {
			p.mark(cell, xs[i], top)
		}
	}
}

func (p *jzPainter) cell(v JazzCell, x, w, top float64) {
	E := p.E
	content := jzInk
	if v.Lead {
		content = jzFaded
	}
	if v.LeadTag && p.sty.LeadWord != "" {
		// .jz-lead-tag: left .3em, bottom .05em of its .36em.
		s := 0.36 * E
		p.text(x+0.3*s, top+jzSysH*E-0.05*s-0.2*s, p.sty.LeadWord, faceHand, s, jzSoft)
	}
	p.barline(v.Left, x, top, true)
	p.barline(v.Right, x+w, top, false)

	if v.Segno {
		s := 0.72 * E // .jz-segno: top 1.2em, left -1.97em
		p.text(x-1.97*s, top+1.2*s+0.5*s, smuflSegno, faceMusic, s, jzInk)
	}
	if v.Coda {
		s := 0.72 * E // .jz-coda: top -1.25em, left -2.1em
		p.text(x-2.1*s, top-1.25*s+0.5*s, smuflCoda, faceMusic, s, jzInk)
	}
	if v.ToCoda {
		s := 0.62 * E // .jz-tocoda: top .1em, right .3em; -.6em over an ending
		t := 0.1 * s
		if v.VoltaSpan > 0 {
			t = -0.6 * s
		}
		gw := p.mw(smuflCoda, faceMusic, s)
		p.text(x+w-0.3*s-gw, top+t+0.5*s, smuflCoda, faceMusic, s, jzInk)
	}
	if v.Name != "" && !v.Lead {
		// .jz-name: left .3em, top -.22em of its .3em; over an ending's
		// bracket, -1.45em. At most 3.8 bars wide.
		s := 0.3 * E
		t := -0.22 * s
		if v.VoltaSpan > 0 {
			t = -1.45 * s
		}
		p.text(x+0.3*s, top+t+0.8*s, p.ellipsis(v.Name, faceHand, s, 3.8*w), faceHand, s, jzSoft)
	}
	if v.TSNum != "" {
		p.timeSig(v, x, top)
	}
	if v.VoltaSpan > 0 {
		p.volta(v, x, w, top)
	}
	if v.Rest {
		s := 1.15 * E // .jz-rest: centred, bottom .95em
		gw := p.mw(smuflWholeRest, faceMusic, s)
		p.text(x+w/2-gw/2, top+jzSysH*E-0.95*s-0.5*s, smuflWholeRest, faceMusic, s, content)
	}
	switch v.Simile {
	case 1:
		p.simile(x+w/2, top, smuflSimile1, 0, content)
	case 2:
		p.simile(x+w/2, top, smuflSimile1, 2, content)
	}
	p.chords(v, x, w, top, content)
	if len(v.Texts) > 0 {
		// .jz-stx: right .2em, bottom -.08em of its .4em.
		s := 0.4 * E
		var parts []string
		for _, t := range v.Texts {
			str := t.S
			if t.WithRepeats && p.sty.WithRepeatsWord != "" {
				str += " (" + p.sty.WithRepeatsWord + ")"
			}
			parts = append(parts, str)
		}
		str := strings.Join(parts, " · ")
		tw := p.mw(str, faceHand, s)
		p.text(x+w-0.2*s-tw, top+jzSysH*E+0.08*s-0.2*s, str, faceHand, s, jzInk)
	}
}

// ellipsis cuts s to maxW with "…" (.jz-name's text-overflow).
func (p *jzPainter) ellipsis(s, face string, size, maxW float64) string {
	if p.mw(s, face, size) <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && p.mw(string(r)+"…", face, size) > maxW {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// barline draws a barline kind at x (.jz-lineL at a bar's left edge,
// .jz-lineR at its right): its strokes from .38em under the system's top
// to .32em over its bottom, .085em apart — thin .045em, thick .15em,
// repeat dots .14em.
func (p *jzPainter) barline(kind string, x, top float64, isLeft bool) {
	if kind == "" {
		return
	}
	E := p.E
	type stroke struct {
		w    float64
		dots bool
		thin bool
	}
	thin := stroke{w: 0.045 * E, thin: true}
	thick := stroke{w: 0.15 * E}
	dots := stroke{w: 0.14 * E, dots: true}
	var parts []stroke
	switch kind {
	case "single":
		parts = []stroke{thin}
	case "sec-start":
		parts = []stroke{thick, thin}
	case "sec-end", "final":
		parts = []stroke{thin, thick}
	case "rep-start":
		parts = []stroke{thick, thin, dots}
	case "rep-end":
		parts = []stroke{dots, thin, thick}
	case "rep-both":
		parts = []stroke{dots, thin, thick, thin, dots}
	default:
		return
	}
	gap := 0.085 * E
	total := -gap
	for _, s := range parts {
		total += s.w + gap
	}
	// Where the group starts: a single line straddles the edge (.02em
	// out), a wider group stands inside the bar, a "both" is centred on
	// the barline.
	var start float64
	switch {
	case kind == "rep-both":
		start = x - total/2
	case kind == "single" && isLeft:
		start = x - 0.02*E
	case kind == "single":
		start = x - total + 0.02*E
	case isLeft:
		start = x
	default:
		start = x - total
	}
	y1, y2 := top+0.38*E, top+(jzSysH-0.32)*E
	for _, s := range parts {
		if s.dots {
			mid := (y1 + y2) / 2
			r := s.w / 2
			p.c.filledCircle(start+r, mid-0.21*E, r, jzInk)
			p.c.filledCircle(start+r, mid+0.21*E, r, jzInk)
		} else {
			p.c.filledRect(start, y1, s.w, y2-y1, jzInk)
		}
		start += s.w + gap
	}
}

// timeSig draws a time signature stacked in the margin before the bar's
// barline (.jz-ts: top 1.25em, translateX(-135%), its .56em, line-height
// .86), or just inside it after a meter change mid-line (.jz-ts.mid).
func (p *jzPainter) timeSig(v JazzCell, x, top float64) {
	s := 0.56 * p.E
	nw, dw := p.mw(v.TSNum, faceHand, s), p.mw(v.TSDen, faceHand, s)
	w := max(nw, dw)
	t, l := top+1.25*s, x-1.35*w
	if v.TSMid {
		t, l = top+0.78*s, x+0.16*s
	}
	lineH := 0.86 * s
	base := t + (0.86-1)/2*s + 0.8*s
	p.text(l+(w-nw)/2, base, v.TSNum, faceHand, s, jzInk)
	p.text(l+(w-dw)/2, base+lineH, v.TSDen, faceHand, s, jzInk)
}

// volta draws an ending's bracket (.jz-volta: top .02em, left .12em,
// span bars less .5em wide, .32em tall, .05em strokes) and its label.
func (p *jzPainter) volta(v JazzCell, x, w, top float64) {
	E := p.E
	lw := 0.05 * E
	l, t := x+0.12*E, top+0.02*E
	r := x + float64(v.VoltaSpan)*w - 0.5*E + 0.12*E
	h := 0.32 * E
	p.c.filledRect(l, t, r-l, lw, jzInk)
	if !v.VoltaCont {
		p.c.filledRect(l, t, lw, h, jzInk)
	}
	if v.VoltaClosed {
		p.c.filledRect(r-lw, t, lw, h, jzInk)
	}
	if v.Volta != "" {
		s := 0.36 * E // .jz-volta .n: left .18em, top .02em
		p.text(l+lw+0.18*s, t+lw+0.02*s+0.8*s, v.Volta, faceHand, s, jzInk)
	}
}

// simile draws a repeat sign centred on cx (.jz-rep1, .jz-rep2: bottom
// .9em of their 1.15em), with a small number over it for "%%".
func (p *jzPainter) simile(cx, top float64, glyph string, n int, col rgb) {
	s := 1.15 * p.E
	gw := p.mw(glyph, faceMusic, s)
	boxBottom := top + jzSysH*p.E - 0.9*s
	p.text(cx-gw/2, boxBottom-0.5*s, glyph, faceMusic, s, col)
	if n > 0 {
		// .jz-rep1 sup: top -.55em, its .38em.
		ss := 0.38 * s
		str := "2"
		sw := p.mw(str, faceHand, ss)
		p.text(cx-sw/2, boxBottom-s-0.55*ss+0.8*ss, str, faceHand, ss, col)
	}
}

// mark draws the rehearsal mark's box at the section's first barline,
// in the margin (.jz-rm: its .56em, right .27em of it from the barline,
// top -.3em; padding .12em .2em .08em, a .08em border; a box of several
// marks wraps after each "/" within 3.4em, growing upwards, its bottom
// 1em under the system's top).
func (p *jzPainter) mark(v JazzCell, x, top float64) {
	s := 0.56 * p.E
	pad, border := 0.2*s, 0.08*s
	var lines []string
	if len(v.MarkParts) > 1 {
		maxText := 3.4*s - 2*pad - 2*border
		cur := ""
		for i, m := range v.MarkParts {
			piece := m
			if i < len(v.MarkParts)-1 {
				piece += "/"
			}
			if cur != "" && p.mw(cur+piece, faceHand, s) > maxText {
				lines = append(lines, cur)
				cur = ""
			}
			cur += piece
		}
		lines = append(lines, cur)
	} else {
		lines = []string{v.MarkParts[0]}
	}
	textW := 0.0
	for _, l := range lines {
		textW = max(textW, p.mw(l, faceHand, s))
	}
	bw := max(1.35*s, textW+2*pad+2*border)
	bh := float64(len(lines))*s + 0.12*s + 0.08*s + 2*border
	right := x - 0.27*s
	t := top - 0.3*s
	if len(v.MarkParts) > 1 {
		t = top + s - bh
	}
	p.c.filledRect(right-bw, t, bw, bh, jzPaper)
	p.c.filledRect(right-bw, t, bw, border, jzInk)
	p.c.filledRect(right-bw, t+bh-border, bw, border, jzInk)
	p.c.filledRect(right-bw, t, border, bh, jzInk)
	p.c.filledRect(right-border, t, border, bh, jzInk)
	for i, l := range lines {
		lw := p.mw(l, faceHand, s)
		base := t + border + 0.12*s + float64(i)*s + 0.8*s
		p.text(right-bw+(bw-lw)/2, base, l, faceHand, s, jzInk)
	}
}

// notes draws a line's "note:" directions under it (.jz-notes: the UI
// font, italic, .3em, soft ink).
func (p *jzPainter) notes(notes []string, y float64) {
	size := jzNoteSize * p.E
	y += 0.1 * size
	for _, words := range p.notesLines(notes) {
		p.text(p.x0+1.5*size, y+0.85*size, strings.Join(words, " "), faceSansIt, size, jzSoft)
		y += size
	}
}

// ---- chords (.jz-at, .jz-ch) ----

// jzGlyph is one piece of a chord, placed from the chord box's top-left.
type jzGlyph struct {
	s, face    string
	size, x, b float64
}

// chordGlyphs lays a chord out as the page's flex boxes do: the root
// (1em, its accidental .62em raised to the letter's top), then the
// quality over the bass in a column .03em after it — the quality .54em,
// raised .12em of it; the bass .6em, .14em of it under the quality, the
// box's height growing with it.
func (p *jzPainter) chordGlyphs(ch JazzChord) (gs []jzGlyph, w, h float64) {
	E := p.E
	if ch.Root.Letter == "" {
		s := 0.62 * E // .jz-nc .jz-r, .jz-txt .jz-r
		return []jzGlyph{{ch.Text, faceHand, s, 0, 0.8 * s}}, p.mw(ch.Text, faceHand, s), s
	}
	x := 0.0
	gs = append(gs, jzGlyph{ch.Root.Letter, faceHand, E, 0, 0.8 * E})
	x += p.mw(ch.Root.Letter, faceHand, E)
	if g := jzAccGlyph(ch.Root.Acc); g != "" {
		s := 0.62 * E // .jz-acc: margin -.18em .02em 0
		x += 0.02 * s
		gs = append(gs, jzGlyph{g, faceMusic, s, x, -0.18*s + 0.5*s})
		x += p.mw(g, faceMusic, s) + 0.02*s
	}
	qx := x + 0.03*E // .jz-qb margin-left
	q := 0.54 * E    // .jz-q
	qTop := -0.12 * q
	qBase := qTop + 0.8*q
	below := 0.2 * q
	qx2 := qx
	for _, r := range ch.Quality {
		switch r.Kind {
		case JazzRunText:
			gs = append(gs, jzGlyph{r.S, faceHand, q, qx2, qBase})
			qx2 += p.mw(r.S, faceHand, q)
		case JazzRunSign, JazzRunMinor:
			s := q
			if r.Kind == JazzRunMinor {
				s = 1.1 * q // .jz-g.jz-min
			}
			gs = append(gs, jzGlyph{r.S, faceMusic, s, qx2, qBase})
			qx2 += p.mw(r.S, faceMusic, s)
			below = max(below, 0.5*s)
		case JazzRunAcc:
			s := 0.95 * q // .jz-acc2: margin 0 .02em
			qx2 += 0.02 * s
			gs = append(gs, jzGlyph{r.S, faceMusic, s, qx2, qBase})
			qx2 += p.mw(r.S, faceMusic, s) + 0.02*s
			below = max(below, 0.5*s)
		}
	}
	qH := max(0.9*q, 0.8*q+below) // min-height .9em
	w = qx2
	h = E
	if ch.Bass.Letter != "" {
		b := 0.6 * E // .jz-b: margin-top .14em, margin-left -.05em
		bTop := qTop + qH + 0.14*b
		bx := qx - 0.05*b
		gs = append(gs, jzGlyph{"/", faceHand, b, bx, bTop + 0.8*b})
		bx += p.mw("/", faceHand, b)
		gs = append(gs, jzGlyph{ch.Bass.Letter, faceHand, b, bx, bTop + 0.8*b})
		bx += p.mw(ch.Bass.Letter, faceHand, b)
		if g := jzAccGlyph(ch.Bass.Acc); g != "" {
			s := 0.9 * b // .jz-b .jz-acc
			bx += 0.02 * s
			gs = append(gs, jzGlyph{g, faceMusic, s, bx, bTop + 0.5*s})
			bx += p.mw(g, faceMusic, s) + 0.02*s
		}
		w = max(w, bx)
		h = max(h, bTop+b)
	}
	return gs, w, h
}

// chords draws a bar's chords, each at its beat (.jz-at: left at x of
// the bar plus its pad, bottom .86em over the system's bottom), squeezed
// when it would run into the next one or out of the bar (song.js
// fitJazzChords, down to .55; here the chord shrinks as a whole).
func (p *jzPainter) chords(v JazzCell, x, w, top float64, col rgb) {
	E := p.E
	bottom := top + (jzSysH-0.86)*E
	lefts := make([]float64, len(v.Chords))
	for i, ch := range v.Chords {
		lefts[i] = x + ch.X*w + ch.Pad*E
	}
	for i, ch := range v.Chords {
		gs, cw, chh := p.chordGlyphs(ch)
		limit := x + w
		if i+1 < len(v.Chords) {
			limit = lefts[i+1]
		}
		limit -= 0.1 * E
		scale := 1.0
		if avail := limit - lefts[i]; cw > avail && avail > 0 {
			scale = max(0.55, avail/cw)
		}
		boxTop := bottom - chh*scale
		for _, g := range gs {
			p.text(lefts[i]+g.x*scale, boxTop+g.b*scale, g.s, g.face, g.size*scale, col)
		}
	}
}
