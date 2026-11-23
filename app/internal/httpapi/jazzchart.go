package httpapi

// The Jazz chart view's model: the song laid out the way an iReal Pro
// chart is — a fixed grid of 4 bars per line, 4 beat cells per bar,
// hand-lettered chords, boxed rehearsal marks, real barlines — with the
// form written in the chart's own road signs (jazzform.go), and, for the
// player, every played bar tied to the printed bar it is read from.

import (
	"fmt"
	"html"
	"html/template"
	"math"
	"strconv"
	"strings"

	"app/internal/chordsym"
	"app/internal/song"
	"app/internal/tab"
)

// SMuFL code points, drawn with the Petaluma music font.
const (
	jzGlyphFlat    = ""
	jzGlyphSharp   = ""
	jzGlyphDim     = ""
	jzGlyphHalfDim = ""
	jzGlyphAug     = ""
	jzGlyphMaj     = ""
	jzGlyphMin     = ""
)

type jazzChart struct {
	Title, Style, Composer string
	Tempo                  string
	Compound               bool // 6/8, 9/8, 12/8: the tempo's beat is a dotted quarter
	Key                    template.HTML
	// The key spoken: its root ("B", "flat") apart from the rest ("minor"),
	// for the page's transpose.
	KeySayRoot, KeySayRest []jzWord
	Lines                  []jzLine
	// Compact: the form is written with repeats/endings/D.S.; false when
	// the chart fell back to printing every section as written.
	Compact bool
	Bars    int // played bars
}

type jzLine struct {
	Lead  int  // narrow cells of the song's lead-in it opens with (jzInsertLead)
	Gap   bool // first line of a printed section: a little room above it
	Coda  bool // the coda's first line: more room above it
	Cells []jzCellView
	Notes []string // the section's "note:" directions, under its first line
}

type jzCellView struct {
	Pad bool

	Played    string // every played bar read from this cell, space-separated
	First     int    // the first of them (data-global-bar)
	HasPlayed bool

	Left, Right string // barline kinds: single, sec-start, sec-end, final, rep-start, rep-end, rep-both

	Mark, Name string   // rehearsal mark and the section's name, at a section's first bar
	MarkParts  []string // the marks of a box for several names (In, V), to wrap after each "/"
	JumpTo     int      // the played bar the mark jumps to

	TSNum, TSDen string // a time signature drawn before this bar
	TSMid        bool

	Volta       string // an ending's label ("1, 2.") on the bracket's first cell on this line
	VoltaSpan   int
	VoltaClosed bool
	VoltaCont   bool

	Segno, Coda, ToCoda bool
	Texts               []jzText

	Chords []jzChordView
	Simile int  // 1: "%", 2: "%%" drawn in one bar
	Pair2  bool // the two-bar repeat sign, straddling this bar's left barline
	Rest   bool // a whole-bar rest
	// Lead: one of the song's "lead:" bars (song.LeadMeasures), played
	// before bar 1 and in no section; LeadTag on the first of them.
	Lead, LeadTag bool
}

type jzText struct {
	S           string
	WithRepeats bool
	Left        bool
}

type jzChordView struct {
	X     string // start, as a fraction of the bar (0..1)
	Pad   string // em
	HTML  template.HTML
	Title string // the chord as written, without its duration ("Dm7")
	Q     string // the quality and extension as written, for the title after a transpose
	// The chord spoken (aria-label): the root, the quality, the bass —
	// apart so that the page's transpose can say the moved root and bass.
	SayRoot, SayQ, SayBass []jzWord
}

// buildJazzChart builds the Jazz chart for sg, whose sections were built
// into sections (buildSections: their played bar numbers, the grid each
// one plays).
func buildJazzChart(sg *song.Song, sections []viewerSection) *jazzChart {
	jf := jazzFormFor(sg, sections)
	if jf == nil {
		return nil
	}
	ch := layoutJazzChart(jf.sg, sections, jf.measures, jf.res, jf.globalOf)
	jzInsertLead(ch, jf.sg)
	return ch
}

// jzInsertLead draws the song's lead-in (its "lead:" bars,
// song.LeadMeasures, in no section and so in none of the form's grids) at
// the start of the chart's first line: narrow cells ahead of bar 1
// (--lead, jazz.css), the song's played bars 0…K-1. The time signature
// and the first section's rehearsal box (in the margin, where every
// line's is) move onto the first of them; the section's name, opening
// barline and start stay on its bar 1 — the box jumps there.
func jzInsertLead(ch *jazzChart, sg *song.Song) {
	leadMs := sg.LeadMeasures()
	k := len(leadMs)
	if ch == nil || k == 0 {
		return
	}
	if len(ch.Lines) == 0 {
		ch.Lines = []jzLine{{}}
	}
	line := &ch.Lines[0]
	f := 0
	for f < len(line.Cells) && line.Cells[f].Pad {
		f++
	}
	// The lead's meter is the first grid's (its row's).
	sec := max(0, tab.FirstGridSection(sg))
	globalTime := orDefault(sg.Get("time"), "4/4")
	ms := map[int][]song.Measure{sec: leadMs}
	cells := make([]jzCell, k)
	for i := range cells {
		cells[i] = newCell(sec, i)
	}
	lead := make([]jzCellView, k)
	for i := range lead {
		v := jzCellView{Lead: true, LeadTag: i == 0, HasPlayed: true, First: i, Played: strconv.Itoa(i), Left: "single"}
		if len(sg.Sections) > 0 {
			jzFillContent(&v, cells[i], ms, cells, i, i > 0, leadMs[i], sg, globalTime)
		} else if isSilentBar(leadMs[i].Tokens) || leadMs[i].Tokens[0] == "R" {
			v.Rest = true
		}
		lead[i] = v
	}
	if f < len(line.Cells) {
		first := &line.Cells[f]
		l := &lead[0]
		l.TSNum, l.TSDen = first.TSNum, first.TSDen
		first.TSNum, first.TSDen, first.TSMid = "", "", false
		if first.Mark != "" {
			l.Mark, l.MarkParts, l.Name, l.JumpTo = first.Mark, first.MarkParts, first.Name, first.JumpTo
			first.Mark, first.MarkParts = "", nil
		}
	}
	line.Cells = append(append(append([]jzCellView(nil), line.Cells[:f]...), lead...), line.Cells[f:]...)
	line.Lead = k
	ch.Bars += k
}

// jazzForm is a song's chart form with what laying it out needs.
type jazzForm struct {
	sg       *song.Song
	measures map[int][]song.Measure
	res      jzFormResult
	globalOf []int // the form's played bars -> the song's played bar numbers
}

func jazzFormFor(sg *song.Song, sections []viewerSection) *jazzForm {
	sg = withRiffOnlyBars(sg)
	globalTime := orDefault(sg.Get("time"), "4/4")
	globalBeats := timeSigBeats(globalTime)
	measures := map[int][]song.Measure{}
	for i, sec := range sg.Sections {
		if len(sec.Measures) > 0 {
			measures[i] = sec.Measures
		}
	}
	keys := map[[2]int]string{}
	key := func(sec, bar int) string {
		k := [2]int{sec, bar}
		if s, ok := keys[k]; ok {
			return s
		}
		m := measures[sec][bar]
		s := fmt.Sprint(normalizeMeasure(m, sectionBeatsPerBar(sg.Sections[sec], globalBeats)))
		keys[k] = s
		return s
	}
	// Grids that are the same music, with the same repeat signs, are one grid.
	canon := map[int]int{}
	for i := range sg.Sections {
		ms, ok := measures[i]
		if !ok {
			continue
		}
		canon[i] = i
		for j := 0; j < i; j++ {
			if c, ok := canon[j]; ok && c == j && jzSameGrid(measures[j], ms) && measuresEqual(measures[j], ms, sectionBeatsPerBar(sg.Sections[j], globalBeats), sectionBeatsPerBar(sg.Sections[i], globalBeats)) {
				canon[i] = j
				break
			}
		}
	}
	classes := map[string]int{}
	var occs []jzOcc
	var expected [][2]int
	var globalOf []int // expected bar -> the song's played bar number
	for i, vs := range sections {
		g := vs.GridFrom
		if vs.BarCount <= 0 || g < 0 || len(measures[g]) == 0 {
			continue
		}
		exp := jzExpand(measures[g])
		if len(exp) == 0 {
			continue
		}
		reps := vs.BarCount / len(exp)
		if reps < 1 {
			reps = 1
		}
		norm := jzNormLabel(vs.Label)
		// One printed section per grid, whatever the sections playing it
		// are called: its box carries every name (In/V).
		ck := strconv.Itoa(canon[g])
		cl, ok := classes[ck]
		if !ok {
			cl = len(classes)
			classes[ck] = cl
		}
		occs = append(occs, jzOcc{sec: i, label: vs.Label, norm: norm, grid: canon[g], reps: reps, class: cl, bars: reps * len(exp)})
		// Its played bars, each read from its grid's written bar
		// (playOrder, as the Song view's rows read them too).
		for k, w := range playOrder(measures[g], reps*len(exp)) {
			expected = append(expected, [2]int{g, w})
			globalOf = append(globalOf, vs.StartBar+k)
		}
	}
	if len(occs) == 0 {
		return nil
	}
	res := buildJazzForm(measures, occs, expected, key)
	return &jazzForm{sg: sg, measures: measures, res: res, globalOf: globalOf}
}

// jzSameGrid: two grids with the same bars and the same repeat signs.
func jzSameGrid(a, b []song.Measure) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Open != b[i].Open || a[i].Close != b[i].Close || a[i].Volta != b[i].Volta || a[i].RepeatCount != b[i].RepeatCount || a[i].Meter != b[i].Meter {
			return false
		}
	}
	return true
}

// jzNormLabel is a section's name without its number or case: "Verse 2"
// and "verse" are the same section.
func jzNormLabel(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	s = strings.TrimRight(s, "0123456789 .:-")
	return strings.Join(strings.Fields(s), " ")
}

// jzMarks gives each section name its rehearsal mark, iReal Pro's way:
// In for an intro, V for a verse, then A, B, C… for the others in the
// order they first come.
type jzMarks struct {
	by   map[string]string
	next int
}

func (m *jzMarks) mark(norm string) string {
	if m.by == nil {
		m.by = map[string]string{}
	}
	if s, ok := m.by[norm]; ok {
		return s
	}
	var s string
	switch {
	case strings.HasPrefix(norm, "intro"):
		s = "In"
	case strings.HasPrefix(norm, "verse"), strings.HasPrefix(norm, "couplet"):
		s = "V"
	default:
		s = jzLetters(m.next)
		m.next++
	}
	m.by[norm] = s
	return s
}

func jzLetters(i int) string {
	if i < 26 {
		return string(rune('A' + i))
	}
	return jzLetters(i/26-1) + string(rune('A'+i%26))
}

// ---- Layout ---------------------------------------------------------------

func layoutJazzChart(sg *song.Song, sections []viewerSection, measures map[int][]song.Measure, res jzFormResult, globalOf []int) *jazzChart {
	globalTime := orDefault(sg.Get("time"), "4/4")
	cells := res.cells
	ch := &jazzChart{
		Title:    sg.Get("title"),
		Style:    sg.Get("style"),
		Composer: sg.Get("artist"),
		Tempo:    strings.TrimSpace(sg.Get("tempo")),
		Compact:  res.compact,
		Bars:     len(res.played),
	}
	num, den := timeSigParts(globalTime)
	ch.Compound = den == "8" && (num == "6" || num == "9" || num == "12")
	ch.Key = jzKeyHTML(sg.Get("key"))
	if k, ok := chordsym.ParseKey(sg.Get("key")); ok {
		ch.KeySayRoot = jzNoteWords(k.Root)
		if k.Minor {
			ch.KeySayRest = []jzWord{{Key: "chord|minor"}}
		}
	} else if key := strings.TrimSpace(sg.Get("key")); key != "" {
		ch.KeySayRoot = []jzWord{{Text: key}}
	}

	// Played bars per cell.
	played := make([][]int, len(cells))
	for i, ci := range res.played {
		g := i
		if i < len(globalOf) {
			g = globalOf[i]
		}
		played[ci] = append(played[ci], g)
	}

	// Notes: each played section's "note:" lines, under the printed
	// section it is read from; a later pass's notes say whose they are.
	blockOf := make([]int, len(cells))
	b := -1
	for i, c := range cells {
		if c.blockStart {
			b = i
		}
		blockOf[i] = b
	}
	// The block an occurrence is read from: the first printed section of
	// its played bars that prints its own grid. An outro whose first bars
	// are read from the chorus (a D.S. to a coda) is the coda's, not the
	// chorus's. -1 when none does (its bars are all read from others).
	ownBlock := func(o jzOcc, bar int) int {
		for k := bar; k < bar+o.bars && k < len(res.played); k++ {
			ci := res.played[k]
			if blk := blockOf[ci]; blk >= 0 && cells[blk].sec == o.grid {
				return blk
			}
		}
		return -1
	}
	// The names a printed section is played under: every section read
	// from it, in the order they come; and how many occurrences are.
	type blockName struct{ norm, label string }
	names := map[int][]blockName{}
	readers := map[int]int{}
	{
		bar := 0
		for _, o := range res.occs {
			if blk := ownBlock(o, bar); blk >= 0 {
				readers[blk]++
				known := false
				for _, n := range names[blk] {
					if n.norm == o.norm {
						known = true
					}
				}
				if !known {
					names[blk] = append(names[blk], blockName{o.norm, o.label})
				}
			}
			bar += o.bars
		}
	}
	// Marks: in the order the printed sections come.
	var marks jzMarks
	for i, c := range cells {
		if !c.blockStart {
			continue
		}
		if len(names[i]) == 0 && c.unit >= 0 {
			names[i] = []blockName{{res.units[c.unit].norm, res.units[c.unit].label}}
		}
		if !c.codaLine || c.unit >= 0 {
			for _, n := range names[i] {
				marks.mark(n.norm)
			}
		}
	}

	// A note goes under the printed section its occurrence is read from;
	// when several occurrences are read from it (a verse played on two
	// passes of a repeat, two names over one grid), each note says whose
	// it is: "Verse 2: loud".
	notes := map[int][]string{}
	{
		bar := 0
		for _, o := range res.occs {
			vs := sections[o.sec]
			if len(vs.Notes) > 0 && bar < len(res.played) {
				blk := ownBlock(o, bar)
				if blk < 0 {
					blk = blockOf[res.played[bar]]
				}
				own := false
				for _, n := range names[blk] {
					if n.norm == o.norm {
						own = true
					}
				}
				for _, note := range vs.Notes {
					text := note
					if blk >= 0 && (readers[blk] > 1 || len(names[blk]) > 1 || !own) {
						name := unescapeLabel(vs.Label)
						if vs.LabelNumber > 0 {
							name = fmt.Sprintf("%s %d", name, vs.LabelNumber)
						}
						text = name + ": " + note
					}
					if !jzContains(notes[blk], text) {
						notes[blk] = append(notes[blk], text)
					}
				}
			}
			bar += o.bars
		}
	}

	// Lines of 4.
	type slot struct{ cell int } // -1: pad
	var lines [][]slot
	var lineStart []int // first cell of each line
	col := make([]int, len(cells))
	var cur []slot
	flush := func() {
		if len(cur) > 0 {
			lines = append(lines, cur)
			cur = nil
		}
	}
	for i, c := range cells {
		if c.blockStart || c.alignWith >= 0 {
			flush()
			if c.alignWith >= 0 {
				for p := 0; p < col[c.alignWith]; p++ {
					cur = append(cur, slot{-1})
				}
			}
		}
		if len(cur) == 0 {
			lineStart = append(lineStart, i)
		}
		col[i] = len(cur)
		cur = append(cur, slot{i})
		if len(cur) == 4 {
			flush()
		}
	}
	flush()

	// Which cell is the last of its printed section.
	blockEnd := make([]bool, len(cells))
	for i := range cells {
		blockEnd[i] = i == len(cells)-1 || cells[i+1].blockStart
	}
	// Endings: each run's extent and whether it closes with a repeat.
	runOf := make([]int, len(cells)) // the ending's first cell, or -1
	for i := range runOf {
		runOf[i] = -1
	}
	// The last ending, the one no repeat sign closes, has no written end:
	// it runs on to the next sign as the form reads it, but its bracket is
	// iReal's short open one, over its first bar or two (no longer than
	// the ending before it), not over the rest of the section.
	prevLen := 0
	for i, c := range cells {
		if !c.endStart {
			continue
		}
		end := c.endRunEnd
		if end >= len(cells) {
			end = len(cells) - 1
		}
		if !cells[end].close {
			span := 2
			if prevLen > 0 && prevLen < span {
				span = prevLen
			}
			end = min(end, i+span-1)
		}
		prevLen = c.endRunEnd - i + 1
		for k := i; k <= end; k++ {
			runOf[k] = i
		}
	}

	curTime := ""
	for li, ln := range lines {
		line := jzLine{}
		first := ln[0].cell
		for _, s := range ln {
			if s.cell >= 0 {
				first = s.cell
				break
			}
		}
		if cells[first].blockStart && li > 0 {
			line.Gap = true
		}
		line.Coda = cells[first].codaLine
		if cells[first].blockStart {
			line.Notes = notes[first]
		}
		for ci, s := range ln {
			if s.cell < 0 {
				line.Cells = append(line.Cells, jzCellView{Pad: true})
				continue
			}
			i := s.cell
			c := cells[i]
			m := measures[c.sec][c.bar]
			v := jzCellView{}
			if ps := played[i]; len(ps) > 0 {
				strs := make([]string, len(ps))
				for k, p := range ps {
					strs[k] = strconv.Itoa(p)
				}
				v.Played, v.First, v.HasPlayed = strings.Join(strs, " "), ps[0], true
			}
			prevOnLine := ci > 0 && ln[ci-1].cell >= 0
			lastOnLine := ci == len(ln)-1
			// Barlines.
			switch {
			case c.open && prevOnLine && cells[i-1].close:
				v.Left = "rep-both"
			case c.open:
				v.Left = "rep-start"
			case c.blockStart:
				v.Left = "sec-start"
			case prevOnLine && cells[i-1].close:
				v.Left = ""
			default:
				v.Left = "single"
			}
			nextOpensHere := !lastOnLine && i+1 < len(cells) && cells[i+1].open && ln[ci+1].cell == i+1
			switch {
			case c.close && nextOpensHere:
				v.Right = ""
			case c.close:
				v.Right = "rep-end"
			case i == len(cells)-1:
				v.Right = "final"
			case blockEnd[i]:
				v.Right = "sec-end"
			case lastOnLine:
				v.Right = "single"
			}
			// Rehearsal mark, signs.
			if c.blockStart && (c.unit >= 0 || !c.codaLine) && len(names[i]) > 0 {
				ms := make([]string, len(names[i]))
				ls := make([]string, len(names[i]))
				for k, n := range names[i] {
					ms[k], ls[k] = marks.mark(n.norm), n.label
				}
				v.Mark, v.Name = strings.Join(ms, "/"), strings.Join(ls, " / ")
				v.MarkParts = ms
			}
			if c.blockStart && v.HasPlayed {
				v.JumpTo = v.First
			}
			v.Segno, v.Coda, v.ToCoda = c.segno, c.coda, c.toCoda
			// Time signatures.
			// A time signature is drawn where the meter changes, in reading
			// order: a section's own time, a bar's {2/4}, and the section's
			// time again after it — once, not on every bar of a {3/4} run,
			// and never over the next section's own time.
			meter := orDefault(sg.Sections[c.sec].Time, globalTime)
			if m.Meter != "" {
				meter = m.Meter
			}
			if i == 0 || meter != curTime {
				v.TSNum, v.TSDen = timeSigParts(meter)
				v.TSMid = ci > 0
			}
			curTime = meter
			// Endings: one bracket per run per line.
			if r := runOf[i]; r >= 0 && (i == r || !prevOnLine || runOf[i-1] != r) {
				span := 1
				for k := ci + 1; k < len(ln) && ln[k].cell >= 0 && runOf[ln[k].cell] == r; k++ {
					span++
				}
				endIdx := ln[ci+span-1].cell
				v.VoltaSpan = span
				v.VoltaCont = i != r
				v.VoltaClosed = endIdx == cells[r].endRunEnd && cells[endIdx].close
				if i == r {
					v.Volta = jzLabelText(cells[r].endLabels)
				}
			}
			// Texts under the bar.
			if c.countText > 0 {
				v.Texts = append(v.Texts, jzText{S: strconv.Itoa(c.countText) + "x"})
			}
			if c.secTimes > 1 {
				v.Texts = append(v.Texts, jzText{S: strconv.Itoa(c.secTimes) + "x"})
			}
			if c.fine {
				v.Texts = append(v.Texts, jzText{S: "Fine"})
			}
			if c.jump != "" {
				v.Texts = append(v.Texts, jzText{S: c.jump, WithRepeats: c.withRepeats})
			}
			// Content.
			jzFillContent(&v, c, measures, cells, i, prevOnLine, m, sg, globalTime)
			line.Cells = append(line.Cells, v)
		}
		// Two-bar repeats: "%%" whose previous bar repeats the bar three
		// back — both bars drawn empty, the sign straddling their barline.
		for ci := 1; ci < len(ln); ci++ {
			a, b := ln[ci-1].cell, ln[ci].cell
			if a < 0 || b < 0 || b != a+1 || cells[a].sec != cells[b].sec || cells[b].bar != cells[a].bar+1 {
				continue
			}
			ms := measures[cells[b].sec]
			k := cells[b].bar
			if len(ms[k].Tokens) != 1 || ms[k].Tokens[0] != "%%" || k < 3 {
				continue
			}
			res := jzResolved(ms)
			pb := ms[k-1]
			if res[k-1] != res[k-3] || pb.Volta != "" || pb.Open || pb.Close || pb.Meter != "" || ms[k].Volta != "" || ms[k].Open || ms[k].Meter != "" {
				continue
			}
			line.Cells[ci-1].Chords, line.Cells[ci-1].Simile, line.Cells[ci-1].Rest = nil, 0, false
			line.Cells[ci].Chords, line.Cells[ci].Simile = nil, 0
			line.Cells[ci].Pair2 = true
		}
		ch.Lines = append(ch.Lines, line)
	}
	return ch
}

func jzContains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func unescapeLabel(s string) string {
	return strings.NewReplacer(`\&`, "&", `\#`, "#", `\_`, "_", `\$`, "$", `\*`, "*", `\@`, "@", `\\`, `\`).Replace(s)
}

// jzResolved is each bar of a grid with "%" and "%%" replaced by the bar
// they repeat, for comparing bars and for printing a bar away from the
// bars it repeats.
func jzResolved(ms []song.Measure) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		switch {
		case len(m.Tokens) == 1 && m.Tokens[0] == "%" && i >= 1:
			out[i] = out[i-1]
		case len(m.Tokens) == 1 && m.Tokens[0] == "%%" && i >= 2:
			out[i] = out[i-2]
		default:
			out[i] = strings.Join(m.Tokens, " ")
		}
	}
	return out
}

// jzFillContent places a bar's chords on its beat cells.
func jzFillContent(v *jzCellView, c jzCell, measures map[int][]song.Measure, cells []jzCell, i int, prevOnLine bool, m song.Measure, sg *song.Song, globalTime string) {
	tokens := m.Tokens
	ms := measures[c.sec]
	// A simile away from the bars it repeats (an ending's or a coda's
	// first bars) is printed as the chords it stands for.
	if len(tokens) == 1 && (tokens[0] == "%" || tokens[0] == "%%") {
		back := 1
		if tokens[0] == "%%" {
			back = 2
		}
		follows := true
		for d := 1; d <= back; d++ {
			if i-d < 0 || cells[i-d].sec != c.sec || cells[i-d].bar != c.bar-d {
				follows = false
			}
		}
		if !follows && c.bar >= back {
			tokens = strings.Fields(jzResolved(ms)[c.bar])
		}
	}
	if len(tokens) == 0 {
		return
	}
	if len(tokens) == 1 {
		switch tokens[0] {
		case "%":
			v.Simile = 1
			return
		case "%%":
			v.Simile = 2
			return
		case "R", "r":
			v.Rest = true
			return
		}
	}
	if m.Lead && isSilentBar(tokens) {
		// A chordless lead bar ("lead: | ? |"): a whole-bar rest.
		v.Rest = true
		return
	}
	meter := m.Meter
	if meter == "" {
		meter = orDefault(sg.Sections[c.sec].Time, globalTime)
	}
	n, d := timeSigParts(meter)
	beats := float64(atoiOr(n, 4)) * 4 / float64(atoiOr(d, 4))
	if beats <= 0 {
		beats = 4
	}
	type tk struct {
		text string
		dur  float64
		hold bool
	}
	tks := make([]tk, len(tokens))
	hasDur := false
	for k, t := range tokens {
		t = strings.TrimSuffix(t, "~")
		var dur float64
		if j := strings.IndexByte(t, ':'); j >= 0 {
			dur = jzDurBeats(t[j+1:])
			t = t[:j]
			if dur > 0 {
				hasDur = true
			}
		}
		tks[k] = tk{text: t, dur: dur, hold: t == "."}
	}
	starts := make([]float64, len(tks))
	pos := 0.0
	for k := range tks {
		if hasDur {
			starts[k] = pos / beats
			if tks[k].dur > 0 {
				pos += tks[k].dur
			} else {
				pos += (beats - pos) / float64(len(tks)-k)
			}
		} else {
			starts[k] = float64(k) / float64(len(tks))
		}
	}
	// Snap the attacks onto the bar's 4 beat cells, as a fixed-grid chart
	// does; more than 4 attacks keep their proportional places.
	var attacks []int
	for k, t := range tks {
		if !t.hold {
			attacks = append(attacks, k)
		}
	}
	if len(attacks) <= 4 {
		snapped := make([]int, len(attacks))
		last := -1
		for j, k := range attacks {
			cell := int(math.Floor(starts[k]*4 + 1e-6))
			if cell <= last {
				cell = last + 1
			}
			snapped[j] = cell
			last = cell
		}
		if last <= 3 {
			for j, k := range attacks {
				starts[k] = float64(snapped[j]) / 4
			}
		}
	}
	firstPad := 0.22
	switch {
	case m.Meter != "" && v.TSMid:
		firstPad = 0.75
	case c.open:
		firstPad = 0.8
	case c.blockStart || v.Left != "single":
		firstPad = 0.45
	}
	if v.TSMid && m.Meter == "" {
		firstPad = 0.75
	}
	for _, k := range attacks {
		pad := 0.1
		if starts[k] == 0 {
			pad = firstPad
		}
		sym := strings.TrimSpace(tks[k].text)
		sayRoot, sayQ, sayBass := jzChordSpoken(sym)
		p := chordsym.Split(sym)
		title := sym
		if p.Root != "" {
			title = chordsym.Glyphs(p.Root) + p.Quality
			if p.Bass != "" {
				title += "/" + chordsym.Glyphs(p.Bass)
			}
		}
		v.Chords = append(v.Chords, jzChordView{
			X:       strconv.FormatFloat(starts[k], 'f', 4, 64),
			Pad:     strconv.FormatFloat(pad, 'f', 2, 64),
			HTML:    jzChordHTML(sym),
			Title:   title,
			Q:       p.Quality,
			SayRoot: sayRoot, SayQ: sayQ, SayBass: sayBass,
		})
	}
}

// jzDurBeats reads a duration tag ("4", "8.", "2") in quarter notes.
func jzDurBeats(d string) float64 {
	dots := strings.Count(d, ".")
	n, err := strconv.Atoi(strings.TrimRight(d, "."))
	if err != nil || n <= 0 {
		return 0
	}
	v := 4.0 / float64(n)
	add := v
	for i := 0; i < dots; i++ {
		add /= 2
		v += add
	}
	return v
}

// ---- Chord symbols -----------------------------------------------------------

// jzNoteHTML is a note name: the letter, its accidental as a music glyph,
// carrying its pitch class (data-pc) so the page's transpose can re-spell
// it (data-jz: as a letter and a glyph).
func jzNoteHTML(name, class string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	// data-name: the note as written (B♭), what the chord's tooltip and
	// spoken name are put back together from after a transpose.
	b.WriteString(`<span class="` + class + `" data-jz="1" data-pc="` + strconv.Itoa(chordsym.PitchClass(name)) + `" data-name="` + html.EscapeString(chordsym.Glyphs(name)) + `">`)
	b.WriteString(`<span class="jz-l">` + html.EscapeString(name[:1]) + `</span>`)
	if len(name) > 1 {
		switch name[1] {
		case 'b':
			b.WriteString(`<span class="jz-acc">` + jzGlyphFlat + `</span>`)
		case '#':
			b.WriteString(`<span class="jz-acc">` + jzGlyphSharp + `</span>`)
		}
	}
	b.WriteString(`</span>`)
	return b.String()
}

// jzExtHTML writes an extension ("7b9", "sus4", "#11"), its "b"/"#"
// before a number as music glyphs.
func jzExtHTML(ext string) string {
	var b strings.Builder
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			b.WriteString(html.EscapeString(buf.String()))
			buf.Reset()
		}
	}
	for i := 0; i < len(ext); i++ {
		c := ext[i]
		next := byte(0)
		if i+1 < len(ext) {
			next = ext[i+1]
		}
		prevLetter := i > 0 && ((ext[i-1] >= 'a' && ext[i-1] <= 'z') || (ext[i-1] >= 'A' && ext[i-1] <= 'Z'))
		if (c == 'b' || c == '#') && next >= '0' && next <= '9' && !(c == 'b' && prevLetter) {
			flush()
			g := jzGlyphFlat
			if c == '#' {
				g = jzGlyphSharp
			}
			b.WriteString(`<span class="jz-acc2">` + g + `</span>`)
			continue
		}
		buf.WriteByte(c)
	}
	flush()
	return b.String()
}

// jzQual is a chord's quality as the chart writes it: its signs, in
// order (minor, then major 7 for a minor-major chord), and the extension.
type jzQual struct {
	minor, maj, halfDim, dim, aug bool
	ext                           string
}

// jzQuality reads a chord quality the iReal Pro way, on top of
// chordsym.DecomposeQualityJazz: minor is "−" and "m7b5" stays a minor
// chord with a ♭5 (−7♭5, as iReal prints it) while a written ø or iReal's
// "h" is half-diminished; iReal's "^" and "M7"/"Maj7" are the major-7
// triangle, after a minor sign too (−△7); a leading "+" is augmented.
func jzQuality(q string) jzQual {
	q = strings.TrimSpace(q)
	var out jzQual
	switch {
	case strings.HasPrefix(q, "ø"):
		out.halfDim, out.ext = true, q[len("ø"):]
	case strings.HasPrefix(q, "h") && (len(q) == 1 || (q[1] >= '0' && q[1] <= '9')):
		out.halfDim, out.ext = true, q[1:]
	case strings.HasPrefix(q, "+"):
		out.aug, out.ext = true, q[1:]
	default:
		dq := chordsym.DecomposeQualityJazz(q)
		out.ext = dq.Ext
		switch dq.Marker {
		case chordsym.MarkerMajor7:
			out.maj = true
		case chordsym.MarkerMinor:
			out.minor = true
		case chordsym.MarkerHalfDim:
			out.halfDim = true
		case chordsym.MarkerDim:
			out.dim = true
		case chordsym.MarkerAug:
			out.aug = true
		}
	}
	if !out.maj && !out.halfDim && !out.dim {
		if rest, ok := jzMajPrefix(out.ext); ok {
			out.maj, out.ext = true, rest
		}
	}
	if out.halfDim && out.ext == "" {
		out.ext = "7"
	}
	return out
}

// jzMajPrefix reads a major-7 sign at the start of an extension: "maj7",
// "Maj7", "MA7", "M7", "^7", "△", "(maj7)" — "M" alone (a major triad,
// in some books) is not one.
func jzMajPrefix(ext string) (string, bool) {
	s := ext
	paren := strings.HasPrefix(s, "(")
	if paren {
		s = s[1:]
	}
	for _, p := range []string{"maj", "Maj", "MAJ", "MA", "ma", "^", "△", "Δ", "M"} {
		if !strings.HasPrefix(s, p) {
			continue
		}
		rest := s[len(p):]
		if p == "M" && (rest == "" || rest[0] < '0' || rest[0] > '9') {
			return ext, false
		}
		if paren {
			rest = strings.Replace(rest, ")", "", 1)
		}
		if rest == "" && p != "^" && p != "△" && p != "Δ" && !paren {
			// "Cmaj": a major triad written out, the triangle alone.
			return "", true
		}
		return rest, true
	}
	return ext, false
}

// jzWord is a piece of a chord's spoken name: a catalog key (translated
// by the template) or literal text.
type jzWord struct{ Key, Text string }

// jzNoteWords is a note name spoken: "B", "flat".
func jzNoteWords(name string) []jzWord {
	if name == "" {
		return nil
	}
	out := []jzWord{{Text: name[:1]}}
	if len(name) > 1 {
		switch name[1] {
		case 'b':
			out = append(out, jzWord{Text: " "}, jzWord{Key: "chord|flat"})
		case '#':
			out = append(out, jzWord{Text: " "}, jzWord{Key: "chord|sharp"})
		}
	}
	return out
}

// jzQualWords is a quality spoken: "minor major 7", "half-diminished",
// "7 flat 9" — what a screen reader says in place of the music glyphs.
func jzQualWords(q jzQual) []jzWord {
	var out []jzWord
	add := func(w jzWord) {
		if len(out) > 0 {
			out = append(out, jzWord{Text: " "})
		}
		out = append(out, w)
	}
	switch {
	case q.halfDim:
		add(jzWord{Key: "chord|half-diminished"})
	case q.dim:
		add(jzWord{Key: "chord|diminished"})
	case q.aug:
		add(jzWord{Key: "chord|augmented"})
	}
	if q.minor {
		add(jzWord{Key: "chord|minor"})
	}
	if q.maj {
		add(jzWord{Key: "chord|major"})
		if q.ext == "" {
			add(jzWord{Text: "7"})
		}
	}
	ext := q.ext
	if q.halfDim && ext == "7" {
		ext = ""
	}
	// The extension, its "b"/"#" before a number said as words.
	var buf strings.Builder
	flush := func() {
		if s := strings.Trim(buf.String(), " ()"); s != "" {
			add(jzWord{Text: s})
		}
		buf.Reset()
	}
	for i := 0; i < len(ext); i++ {
		c := ext[i]
		next := byte(0)
		if i+1 < len(ext) {
			next = ext[i+1]
		}
		prevLetter := i > 0 && ((ext[i-1] >= 'a' && ext[i-1] <= 'z') || (ext[i-1] >= 'A' && ext[i-1] <= 'Z'))
		if (c == 'b' || c == '#') && next >= '0' && next <= '9' && !(c == 'b' && prevLetter) {
			flush()
			if c == 'b' {
				add(jzWord{Key: "chord|flat"})
			} else {
				add(jzWord{Key: "chord|sharp"})
			}
			continue
		}
		if c == '(' || c == ')' || c == ',' {
			flush()
			continue
		}
		buf.WriteByte(c)
	}
	flush()
	return out
}

// jzChordHTML draws one chord symbol the fake-book way: a big root, its
// quality raised (− △ ø ° +, then the extension), a slash bass smaller
// and below the quality.
func jzChordHTML(sym string) template.HTML {
	sym = strings.TrimSpace(sym)
	switch strings.ToUpper(strings.TrimRight(sym, ".")) {
	case "N.C", "NC":
		return template.HTML(`<span class="jz-ch jz-nc"><span class="jz-r">N.C.</span></span>`)
	}
	p := chordsym.Split(sym)
	if p.Root == "" {
		return template.HTML(`<span class="jz-ch jz-txt"><span class="jz-r">` + html.EscapeString(sym) + `</span></span>`)
	}
	var b strings.Builder
	b.WriteString(`<span class="jz-ch">`)
	b.WriteString(jzNoteHTML(p.Root, "jz-root"))
	b.WriteString(`<span class="jz-qb"><span class="jz-q">`)
	q := jzQuality(p.Quality)
	switch {
	case q.halfDim:
		b.WriteString(`<span class="jz-g">` + jzGlyphHalfDim + `</span>`)
	case q.dim:
		b.WriteString(`<span class="jz-g">` + jzGlyphDim + `</span>`)
	case q.aug:
		b.WriteString(`<span class="jz-g">` + jzGlyphAug + `</span>`)
	}
	if q.minor {
		b.WriteString(`<span class="jz-g jz-min">` + jzGlyphMin + `</span>`)
	}
	if q.maj {
		b.WriteString(`<span class="jz-g">` + jzGlyphMaj + `</span>`)
	}
	b.WriteString(jzExtHTML(q.ext))
	b.WriteString(`</span>`)
	if p.Bass != "" {
		b.WriteString(`<span class="jz-b">/` + jzNoteHTML(p.Bass, "jz-root jz-broot") + `</span>`)
	}
	b.WriteString(`</span></span>`)
	return template.HTML(b.String())
}

// jzChordSpoken is a chord's name for a screen reader, in three parts the
// page's transpose can put back together around a moved root and bass:
// the root, the quality ("minor 7"), the bass. A symbol with no root
// (N.C., a word) is said as written, all in the root's part.
func jzChordSpoken(sym string) (root, qual, bass []jzWord) {
	sym = strings.TrimSpace(sym)
	switch strings.ToUpper(strings.TrimRight(sym, ".")) {
	case "N.C", "NC":
		return []jzWord{{Key: "chord|no chord"}}, nil, nil
	}
	p := chordsym.Split(sym)
	if p.Root == "" {
		return []jzWord{{Text: sym}}, nil, nil
	}
	return jzNoteWords(p.Root), jzQualWords(jzQuality(p.Quality)), jzNoteWords(p.Bass)
}

// jzKeyHTML is the key under the composer, its root transposable.
func jzKeyHTML(key string) template.HTML {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	p := chordsym.Split(key)
	if p.Root == "" {
		return template.HTML(html.EscapeString(key))
	}
	rest := strings.TrimPrefix(key, p.Root)
	return template.HTML(jzNoteHTML(p.Root, "jz-root jz-keyroot") + html.EscapeString(rest))
}
