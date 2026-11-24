package httpapi

// The Jazz chart's PDF: the chart the song page draws (buildJazzChart,
// the "jazz-chart" template) handed to pdfnative as is — its lines,
// cells, barlines, signs and chords on their beats — for pdfnative's
// jazz.go to draw on A4 the way jazz.css draws it on screen. The layout
// is decided once, here, for both.

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"app/internal/chordsym"
	"app/internal/i18n"
	"app/internal/pdfnative"
	"app/internal/song"
)

// jazzChartForPDF builds variantID's Jazz chart from sg (its body, maybe
// moved to a songbook's key) the way the song page does — with the
// bars its Guitar Pro file plays where the song only points into it —
// in lang's words.
func (s *Server) jazzChartForPDF(ctx context.Context, variantID int64, sg *song.Song, lang string) *pdfnative.JazzChart {
	if variantID > 0 {
		sg = withGPOnlyBars(sg, s.loadSongGP(ctx, variantID).Facts)
	}
	return jazzPDFChart(sg, buildJazzChart(sg, buildSections(sg)), lang)
}

// withJazzCharts gives each set list or songbook entry its Jazz chart
// when the export prints Jazz charts (?mode=jazz).
func (s *Server) withJazzCharts(r *http.Request, refs []pdfnative.SongRef) {
	if optionsFromRequest(r).Mode != pdfnative.ModeJazz {
		return
	}
	lang := s.langOfRequest(r)
	for i := range refs {
		vid, _ := strconv.ParseInt(refs[i].Stem, 10, 64)
		refs[i].Jazz = s.jazzChartForPDF(r.Context(), vid, song.Parse(refs[i].Body), lang)
	}
}

// jazzPDFChart is ch for pdfnative; a song with no chart (no grid) is
// its header alone, as its Jazz view shows no bars.
func jazzPDFChart(sg *song.Song, ch *jazzChart, lang string) *pdfnative.JazzChart {
	out := &pdfnative.JazzChart{
		Title:           sg.Get("title"),
		Style:           sg.Get("style"),
		Composer:        sg.Get("artist"),
		Tempo:           strings.TrimSpace(sg.Get("tempo")),
		Compound:        song.CompoundTime(sg.Get("time")),
		KeyWord:         i18n.T(lang, "Key"),
		WithRepeatsWord: i18n.T(lang, "with repeats"),
		LeadWord:        i18n.T(lang, "lead"),
	}
	if key := strings.TrimSpace(sg.Get("key")); key != "" {
		if p := chordsym.Split(key); p.Root != "" {
			out.KeyRoot, out.Key = jzPDFNote(p.Root), strings.TrimPrefix(key, p.Root)
		} else {
			out.Key = key
		}
	}
	if ch == nil {
		return out
	}
	out.Title, out.Style, out.Composer, out.Tempo, out.Compound = ch.Title, ch.Style, ch.Composer, ch.Tempo, ch.Compound
	for _, ln := range ch.Lines {
		pl := pdfnative.JazzLine{Lead: ln.Lead, Gap: ln.Gap, Coda: ln.Coda, Notes: ln.Notes}
		for _, c := range ln.Cells {
			pc := pdfnative.JazzCell{
				Pad: c.Pad, Left: c.Left, Right: c.Right,
				TSNum: c.TSNum, TSDen: c.TSDen, TSMid: c.TSMid,
				Volta: c.Volta, VoltaSpan: c.VoltaSpan, VoltaClosed: c.VoltaClosed, VoltaCont: c.VoltaCont,
				Segno: c.Segno, Coda: c.Coda, ToCoda: c.ToCoda,
				Simile: c.Simile, Pair2: c.Pair2, Rest: c.Rest,
				Lead: c.Lead, LeadTag: c.LeadTag,
			}
			if c.Mark != "" {
				pc.MarkParts = c.MarkParts
				if len(pc.MarkParts) == 0 {
					pc.MarkParts = []string{c.Mark}
				}
			}
			if c.Name != "" {
				pc.Name = unescapeLabel(c.Name)
			}
			for _, t := range c.Texts {
				pc.Texts = append(pc.Texts, pdfnative.JazzText{S: t.S, WithRepeats: t.WithRepeats})
			}
			for _, cv := range c.Chords {
				x, _ := strconv.ParseFloat(cv.X, 64)
				pad, _ := strconv.ParseFloat(cv.Pad, 64)
				pc.Chords = append(pc.Chords, jzPDFChord(cv.sym, x, pad))
			}
			pl.Cells = append(pl.Cells, pc)
		}
		out.Lines = append(out.Lines, pl)
	}
	return out
}

// jzPDFChord is one chord as jzChordHTML draws it.
func jzPDFChord(sym string, x, pad float64) pdfnative.JazzChord {
	out := pdfnative.JazzChord{X: x, Pad: pad}
	sym = strings.TrimSpace(sym)
	switch strings.ToUpper(strings.TrimRight(sym, ".")) {
	case "N.C", "NC":
		out.Text = "N.C."
		return out
	}
	p := chordsym.Split(sym)
	if p.Root == "" {
		out.Text = sym
		return out
	}
	out.Root, out.Bass = jzPDFNote(p.Root), jzPDFNote(p.Bass)
	for _, r := range jzQualRuns(jzQuality(p.Quality)) {
		k := pdfnative.JazzRunText
		switch r.kind {
		case jzRunSign:
			k = pdfnative.JazzRunSign
		case jzRunMinor:
			k = pdfnative.JazzRunMinor
		case jzRunAcc:
			k = pdfnative.JazzRunAcc
		}
		out.Quality = append(out.Quality, pdfnative.JazzRun{S: r.s, Kind: k})
	}
	return out
}

// jzPDFNote is a note name as jzNoteHTML draws it: the letter, then its
// accidental.
func jzPDFNote(name string) pdfnative.JazzNote {
	if name == "" {
		return pdfnative.JazzNote{}
	}
	n := pdfnative.JazzNote{Letter: name[:1]}
	if len(name) > 1 && (name[1] == 'b' || name[1] == '#') {
		n.Acc = string(name[1])
	}
	return n
}
