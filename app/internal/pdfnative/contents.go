package pdfnative

import (
	"strconv"
	"strings"

	"github.com/signintech/gopdf"
)

// contentsEntry is one row of a setlist/songbook's running-order table.
type contentsEntry struct {
	Title, Artist, Key, Tempo, Group string
	Page                             int // 0 when no page column is shown (the standalone setlist-page export)
}

// renderContentsPage draws the linked title/key/tempo[/page] table, grouped
// by Set with a visible break (blank row + rule) between groups and
// "Encore" (or any other group name) working as just another label — the
// native equivalent of v2/lib/book.typ's contents(). Always two columns:
// a 60+ song songbook fits one page that way, the same reason the Typst
// pipeline it replaces made the same choice.
func renderContentsPage(doc *gopdf.GoPdf, title, subtitle string, entries []contentsEntry, withPage bool, th theme) {
	doc.AddPage()
	paintPageBackground(doc, th)

	y := marginY
	doc.SetFont(faceSerif, "", 22)
	doc.SetTextColor(th.ink.r, th.ink.g, th.ink.b)
	doc.SetXY(marginX, y+16)
	doc.Text(title)

	countLabel := strconv.Itoa(len(entries)) + " SONGS"
	doc.SetFont(faceSansBold, "", 8.5)
	cw, _ := doc.MeasureTextWidth(countLabel)
	doc.SetTextColor(th.meta.r, th.meta.g, th.meta.b)
	doc.SetXY(pageW-marginX-cw, y+14)
	doc.Text(countLabel)
	y += 22

	if subtitle != "" {
		doc.SetFont(faceSans, "", 10.5)
		doc.SetTextColor(th.meta.r, th.meta.g, th.meta.b)
		doc.SetXY(marginX, y+10)
		doc.Text(subtitle)
		y += 16
	}
	doc.SetStrokeColor(th.ink.r, th.ink.g, th.ink.b)
	doc.SetLineWidth(1.3)
	doc.Line(marginX, y, pageW-marginX, y)
	y += 10

	c := newCanvas(doc, true, th, 2, false)
	c.top = y
	c.y = y
	c.bottom = contentBottom()

	// Row height shrinks (never the song-page approach's "allow a 2nd
	// page") so a songbook of any realistic size stays a single contents
	// page, the same deliberate choice v2/lib/book.typ's own two-column
	// contents() made — this is also what lets ExportSetlist number song
	// pages deterministically (cover=1, title=2, contents=3, songs start
	// at 4) without a two-pass render.
	headerH := 11.0
	groups := 0
	var lastG string
	for i, e := range entries {
		if e.Group != "" && (i == 0 || e.Group != lastG) {
			groups++
			lastG = e.Group
		}
	}
	avail := (c.bottom-c.top-headerH-4)*2 - float64(groups)*13
	rowH := 15.0
	for _, candidate := range []float64{15, 13, 11.5, 10, 9, 8} {
		rowH = candidate
		if avail/rowH >= float64(len(entries)) {
			break
		}
	}

	drawHeader := func() {
		c.ensureSpace(headerH + 4)
		doc.SetFont(faceSansBold, "", 7)
		doc.SetTextColor(th.meta.r, th.meta.g, th.meta.b)
		cols := []struct {
			label string
			x     float64
		}{{"#", 0}, {"TITLE", 18}, {"KEY", c.colW - 76}, {"TEMPO", c.colW - 50}}
		if withPage {
			cols = append(cols, struct {
				label string
				x     float64
			}{"PAGE", c.colW - 18})
		}
		for _, col := range cols {
			doc.SetXY(c.curX()+col.x, c.y+headerH)
			doc.Text(col.label)
		}
		c.y += headerH + 3
		c.line(c.curX(), c.y, c.curX()+c.colW, c.y, th.line, 0.5)
		c.y += 3
	}
	drawHeader()

	var lastGroup string
	first := true
	for i, e := range entries {
		if e.Group != "" && (first || e.Group != lastGroup) {
			if !first {
				c.ensureSpace(rowH + 10)
				c.line(c.curX(), c.y, c.curX()+c.colW, c.y, th.line, 0.5)
				c.y += 10
			}
			c.ensureSpace(13)
			doc.SetFont(faceSansBold, "", 8.5)
			doc.SetTextColor(accentAmber.r, accentAmber.g, accentAmber.b)
			doc.SetXY(c.curX(), c.y+9)
			doc.Text(strings.ToUpper(e.Group))
			c.y += 13
			lastGroup = e.Group
		}
		first = false

		c.ensureSpace(rowH)
		baseline := c.y + rowH*0.68
		// Row font sizes scale with rowH (floored at 0.6x) so a large
		// songbook that needed smaller rows to stay on one page doesn't
		// end up with text taller than its own row.
		fsz := rowH / 15.0
		if fsz > 1 {
			fsz = 1
		} else if fsz < 0.6 {
			fsz = 0.6
		}
		doc.SetFont(faceSans, "", 9*fsz)
		doc.SetTextColor(th.meta.r, th.meta.g, th.meta.b)
		doc.SetXY(c.curX(), baseline)
		doc.Text(strconv.Itoa(i + 1))

		doc.SetFont(faceSansBold, "", 9.5*fsz)
		doc.SetTextColor(th.ink.r, th.ink.g, th.ink.b)
		doc.SetXY(c.curX()+18, baseline)
		title := e.Title
		// The title/artist run starts at x=18 (from curX()) and must stay
		// clear of the KEY column, which starts at c.colW-76 — so the
		// available width is (colW-76) - 18, minus a small gap, not
		// colW-58: that stale boundary sat 22pt too far right and let
		// text run under the KEY/TEMPO/PAGE columns (confirmed against a
		// real 66-song export: "While My Guitar Gently Weeps / The
		// Beatles" ran straight into "115").
		maxTitleW := c.colW - 18 - 76 - 4
		tw, _ := doc.MeasureTextWidth(title)
		for tw > maxTitleW && len(title) > 1 {
			title = title[:len(title)-1]
			tw, _ = doc.MeasureTextWidth(title + "…")
		}
		if title != e.Title {
			title += "…"
		}
		doc.Text(title)
		if e.Artist != "" {
			tw2, _ := doc.MeasureTextWidth(title + "  ")
			doc.SetFont(faceSans, "", 7.5*fsz)
			doc.SetTextColor(th.meta.r, th.meta.g, th.meta.b)
			// Clip to the same right boundary the title itself is held to
			// (maxTitleW, measured from the row's left edge) — unbounded,
			// a long artist name ran straight into the KEY/TEMPO/PAGE
			// columns (e.g. "While My Guitar Gently Weeps / The Beatles").
			artist := e.Artist
			maxArtistW := maxTitleW - tw2
			if maxArtistW >= 8 {
				aw, _ := doc.MeasureTextWidth(artist)
				for aw > maxArtistW && len(artist) > 1 {
					artist = artist[:len(artist)-1]
					aw, _ = doc.MeasureTextWidth(artist + "…")
				}
				if artist != e.Artist {
					artist += "…"
				}
				doc.SetXY(c.curX()+18+tw2, baseline)
				doc.Text(artist)
			}
		}

		if e.Key != "" {
			doc.SetFont(faceSansBold, "", 8.5*fsz)
			doc.SetTextColor(accentAmber.r, accentAmber.g, accentAmber.b)
			doc.SetXY(c.curX()+c.colW-76, baseline)
			doc.Text(e.Key)
		}
		if e.Tempo != "" {
			doc.SetFont(faceSans, "", 8.5*fsz)
			doc.SetTextColor(th.meta.r, th.meta.g, th.meta.b)
			doc.SetXY(c.curX()+c.colW-50, baseline)
			doc.Text(e.Tempo)
		}
		if withPage && e.Page > 0 {
			doc.SetFont(faceSansBold, "", 8.5*fsz)
			doc.SetTextColor(th.meta.r, th.meta.g, th.meta.b)
			doc.SetXY(c.curX()+c.colW-18, baseline)
			doc.Text(strconv.Itoa(e.Page))
		}
		c.y += rowH
		c.line(c.curX(), c.y, c.curX()+c.colW, c.y, th.line, 0.35)
	}
}
