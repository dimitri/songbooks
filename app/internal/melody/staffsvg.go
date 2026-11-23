package melody

import (
	"fmt"
	"html"
	"math"
	"strings"
)

// Tiny staves for a songbook's rows (docs/coaching.md, "The song's melody
// on songbook rows"): the melody's range as one bar, its lowest and
// highest notes as whole notes; or, the melody analysed, the bar where
// its lowest note is first sung and the one where its highest is, each
// as sung (its rhythm, its rests) with the syllables under it, the
// extreme note marked. Drawn here as small SVGs, not by alphaTab: a
// songbook lists dozens of rows, and these are a few lines each.
//
// They draw in currentColor (the page's ink); the marked note carries
// the class "rr-hi", the staff lines "rr-staff", for the page's CSS.

// Extremes is the first lowest and the first highest note of the sheet
// (indexes into Notes), played order: the sure notes only when there are
// some. ok false: no notes.
func (sh Sheet) Extremes() (low, high int, ok bool) {
	return sh.ExtremesWithin(0, 0)
}

// ExtremesWithin is Extremes among the notes from lo to hi (MIDI, the
// recording's key; 0, 0: all): the melody's range as it's known (its
// sung notes' 5th to 95th percentiles, song_melody_ranges) leaves out a
// note the pitch tracking took an octave off. None in it: all of them.
func (sh Sheet) ExtremesWithin(lo, hi int) (low, high int, ok bool) {
	pick := func(bounded bool) (int, int) {
		sure := false
		for _, n := range sh.Notes {
			sure = sure || (!n.Low && (!bounded || (n.Midi >= lo && n.Midi <= hi)))
		}
		l, h := -1, -1
		for i, n := range sh.Notes {
			if (sure && n.Low) || (bounded && (n.Midi < lo || n.Midi > hi)) {
				continue
			}
			if l < 0 || n.Midi < sh.Notes[l].Midi || (n.Midi == sh.Notes[l].Midi && n.Start < sh.Notes[l].Start) {
				l = i
			}
			if h < 0 || n.Midi > sh.Notes[h].Midi || (n.Midi == sh.Notes[h].Midi && n.Start < sh.Notes[h].Start) {
				h = i
			}
		}
		return l, h
	}
	low, high = -1, -1
	if lo > 0 && hi >= lo {
		low, high = pick(true)
	}
	if low < 0 {
		low, high = pick(false)
	}
	return low, high, low >= 0
}

// BarOf is the index in Bars of the bar note n starts in, -1 when it's in
// none.
func (sh Sheet) BarOf(n int) int {
	if n < 0 || n >= len(sh.Notes) {
		return -1
	}
	for i, b := range sh.Bars {
		for _, ev := range b.Events {
			if ev.Note == n && !ev.TieIn {
				return i
			}
		}
	}
	return -1
}

// The staff's geometry, in SVG units: a space between two lines is sp.
const (
	sp        = 5.0
	headRx    = 3.2
	headRy    = 2.35
	stemLen   = 3.5 * sp
	staffLeft = 1.0
	clefW     = 12.0
	svgScale  = 1.25 // CSS pixels a unit
)

// letters' places in an octave (C=0 … B=6) and their pitch classes.
var stepIndex = map[string]int{"C": 0, "D": 1, "E": 2, "F": 3, "G": 4, "A": 5, "B": 6}

// pos is a written note's place on the staff: diatonic steps from C0,
// moved an octave up on a treble-8vb staff (written an octave above it
// sounds).
func pos(step string, octave int, low bool) int {
	d := octave*7 + stepIndex[step]
	if low {
		d += 7
	}
	return d
}

// Treble staff: its bottom line is E4 (pos 30), its top F5 (38), its
// middle B4 (34).
const (
	bottomLine = 30
	topLine    = 38
	middleLine = 34
)

// staff draws an SVG's staff: lines, clef, ledger lines as needed; y of a
// place; the drawing's box.
type staff struct {
	b          strings.Builder
	under      strings.Builder // the five lines, drawn first
	top        float64         // y of the top line
	low        bool            // treble-8vb
	minY, maxY float64
	width      float64
}

func newStaff(low bool) *staff {
	return &staff{top: 0, low: low, minY: -2, maxY: 4*sp + 2}
}

func (s *staff) y(p int) float64 {
	return s.top + float64(topLine-p)*sp/2
}

func (s *staff) see(y float64) {
	s.minY = math.Min(s.minY, y)
	s.maxY = math.Max(s.maxY, y)
}

// ledgers draws the ledger lines a note at place p needs, centred on x.
func (s *staff) ledgers(x float64, p int) {
	for l := bottomLine - 2; l >= p; l -= 2 {
		y := s.y(l)
		fmt.Fprintf(&s.b, `<line class="rr-staff" stroke="currentColor" stroke-width="0.6" stroke-opacity="0.6" x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`, x-headRx-1.6, x+headRx+1.6, y, y)
	}
	for l := topLine + 2; l <= p; l += 2 {
		y := s.y(l)
		fmt.Fprintf(&s.b, `<line class="rr-staff" stroke="currentColor" stroke-width="0.6" stroke-opacity="0.6" x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`, x-headRx-1.6, x+headRx+1.6, y, y)
	}
}

// clef draws a treble clef (with its 8 under it for treble-8vb) at x.
func (s *staff) clef(x float64) {
	g := s.y(32) // the G line
	fmt.Fprintf(&s.b, `<path class="rr-clef" fill="none" stroke="currentColor" stroke-width="1.15" stroke-linecap="round" d="M%.1f %.1f c-2.2 .9 -3.4 -2.6 -.9 -2.9 M%.1f %.1f L%.1f %.1f c2.6 -4.2 4.9 3.1 -.6 8.1 c-4.9 4.4 -4.6 11.4 .9 11.2 c5.1 -.2 4.7 -7.4 -.1 -7.1 c-3.2 .2 -3.1 4.6 .2 4.4"/>`,
		x+3.6, g+10.6, x+3.6, g+10.6, x+5.2, g-17.8)
	s.see(g - 20)
	s.see(g + 11)
	if s.low {
		fmt.Fprintf(&s.b, `<text x="%.1f" y="%.1f" font-size="5.5" text-anchor="middle" fill="currentColor">8</text>`, x+3.6, g+16.5)
		s.see(g + 17)
	}
}

// lines draws the five lines from x0 to x1, and a bar line at each end.
func (s *staff) lines(x0, x1 float64) {
	for l := 0; l < 5; l++ {
		y := s.top + float64(l)*sp
		fmt.Fprintf(&s.under, `<line class="rr-staff" stroke="currentColor" stroke-width="0.6" stroke-opacity="0.6" x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`, x0, x1, y, y)
	}
	fmt.Fprintf(&s.under, `<line class="rr-staff" stroke="currentColor" stroke-width="0.6" stroke-opacity="0.6" x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f"/>`, x1, x1, s.top, s.top+4*sp)
}

// accidental draws ♯, ♭ or ♮ left of a head at x.
func (s *staff) accidental(x, y float64, alter int, natural bool, cls string) {
	glyph := ""
	switch {
	case natural:
		glyph = "♮"
	case alter > 0:
		glyph = "♯"
	case alter < 0:
		glyph = "♭"
	}
	if glyph == "" {
		return
	}
	fmt.Fprintf(&s.b, `<text%s x="%.1f" y="%.1f" font-size="8.5" text-anchor="end" fill="currentColor">%s</text>`, cls, x-headRx-0.8, y+2.8, glyph)
}

// head draws a note head (open for a half or a whole note).
func (s *staff) head(x, y float64, open, whole bool, cls string) {
	rx := headRx
	if whole {
		rx += 0.5
	}
	if open {
		fmt.Fprintf(&s.b, `<ellipse%s cx="%.1f" cy="%.1f" rx="%.2f" ry="%.2f" transform="rotate(-20 %.1f %.1f)" fill="none" stroke="currentColor" stroke-width="1.1"/>`, cls, x, y, rx-0.45, headRy-0.45, x, y)
		return
	}
	fmt.Fprintf(&s.b, `<ellipse%s cx="%.1f" cy="%.1f" rx="%.2f" ry="%.2f" transform="rotate(-20 %.1f %.1f)" fill="currentColor"/>`, cls, x, y, rx, headRy, x, y)
}

// stemAndFlags draws a note's stem and its flags (1 an eighth, 2 a
// sixteenth), up below the middle line, down from it.
func (s *staff) stemAndFlags(x, y float64, p, flags int, cls string) {
	up := p < middleLine
	if up {
		sx := x + headRx - 0.4
		y2 := y - stemLen
		fmt.Fprintf(&s.b, `<line%s x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="currentColor" stroke-width="0.9"/>`, cls, sx, sx, y-0.6, y2)
		for f := 0; f < flags; f++ {
			fy := y2 + float64(f)*3.2
			fmt.Fprintf(&s.b, `<path%s fill="none" stroke="currentColor" stroke-width="1.1" d="M%.1f %.1f c1.2 2.6 4.4 3.4 3.2 8"/>`, cls, sx, fy)
		}
		s.see(y2)
		return
	}
	sx := x - headRx + 0.4
	y2 := y + stemLen
	fmt.Fprintf(&s.b, `<line%s x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="currentColor" stroke-width="0.9"/>`, cls, sx, sx, y+0.6, y2)
	for f := 0; f < flags; f++ {
		fy := y2 - float64(f)*3.2
		fmt.Fprintf(&s.b, `<path%s fill="none" stroke="currentColor" stroke-width="1.1" d="M%.1f %.1f c1.2 -2.6 4.4 -3.4 3.2 -8"/>`, cls, sx, fy)
	}
	s.see(y2)
}

// rest draws a rest of ticks sixteenths at x.
func (s *staff) rest(x float64, ticks int) {
	mid := s.top + 2*sp
	base := ticks
	if d, ok := durations[ticks]; ok && d.dot {
		base = ticks * 2 / 3
	}
	switch base {
	case 16:
		fmt.Fprintf(&s.b, `<rect x="%.1f" y="%.1f" width="5" height="2.4" fill="currentColor"/>`, x-2.5, s.top+sp)
	case 8:
		fmt.Fprintf(&s.b, `<rect x="%.1f" y="%.1f" width="5" height="2.4" fill="currentColor"/>`, x-2.5, mid-2.4)
	case 4:
		fmt.Fprintf(&s.b, `<path fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round" d="M%.1f %.1f l2.2 3 l-2.6 3 l2.6 3.2 c-2.4 -1 -3.8 .4 -1.6 2.6"/>`, x-1, mid-7)
	default: // an eighth, a sixteenth
		flags := 1
		if base <= 1 {
			flags = 2
		}
		fmt.Fprintf(&s.b, `<line x1="%.1f" x2="%.1f" y1="%.1f" y2="%.1f" stroke="currentColor" stroke-width="0.9"/>`, x+1.6, x-0.6, mid-3, mid+5)
		for f := 0; f < flags; f++ {
			fy := mid - 3 + float64(f)*3
			fmt.Fprintf(&s.b, `<path fill="currentColor" d="M%.1f %.1f c-.8 1.2 -2.8 1.2 -3.6 .2 a1.1 1.1 0 1 1 .8 -1.6 c.6 .8 1.8 1.4 2.8 1.4z"/>`, x+1.6, fy)
		}
	}
	if d, ok := durations[ticks]; ok && d.dot {
		fmt.Fprintf(&s.b, `<circle cx="%.1f" cy="%.1f" r="0.8" fill="currentColor"/>`, x+4.2, mid-sp/2)
	}
}

// svg wraps what's drawn in its <svg>, sized from what was seen, with a
// line of text under it (the syllables, their baseline at textY; none:
// 0).
func (s *staff) svg(label string, textY float64) string {
	y0, y1 := s.extent(textY)
	return s.svgIn(label, y0, y1)
}

// extent is the drawing's top and bottom.
func (s *staff) extent(textY float64) (float64, float64) {
	y1 := s.maxY + 2
	if textY > 0 {
		y1 = math.Max(y1, textY+2.5)
	}
	return s.minY - 2, y1
}

// svgIn is svg from y0 to y1: staves drawn side by side share them, so
// their lines are level and the same size.
func (s *staff) svgIn(label string, y0, y1 float64) string {
	h := y1 - y0
	return fmt.Sprintf(`<svg class="rr-svg" xmlns="http://www.w3.org/2000/svg" viewBox="0 %.1f %.1f %.1f" width="%.0f" height="%.0f" role="img" aria-label="%s" focusable="false">%s</svg>`,
		y0, s.width, h, s.width*svgScale, h*svgScale, html.EscapeString(label), s.under.String()+s.b.String())
}

// RangeSVG is the melody's range as one bar: its lowest and its highest
// note, sounding MIDI notes moved by transpose, as whole notes, spelled
// in key; on treble-8vb when the voice is low (as the melody sheet
// writes it: its middle under C4).
func RangeSVG(lowMidi, highMidi, transpose int, key Key, label string) string {
	lo, hi := lowMidi+transpose, highMidi+transpose
	s := newStaff((lo+hi)/2 < lowClef)
	s.clef(staffLeft + 1)
	x := staffLeft + clefW + 7
	for i, m := range []int{lo, hi} {
		step, alter, oct := key.spell(m)
		p := pos(step, oct, s.low)
		y := s.y(p)
		s.ledgers(x, p)
		s.accidental(x, y, alter, false, "")
		s.head(x, y, true, true, "")
		s.see(y - headRy)
		s.see(y + headRy)
		if i == 0 {
			x += 14
		}
	}
	s.width = x + 9
	s.lines(staffLeft, s.width-1)
	return s.svg(label, 0)
}

// BarSVG is one bar of the score as sung: bar (an index into its
// sheet's Bars), its notes spelled in its key, moved by its Transpose,
// on treble-8vb when the melody is low, the notes in marks (indexes into
// Sheet.Notes) marked ("rr-hi"), the syllables under the notes when the
// score has them.
func (sc Score) BarSVG(bar int, marks map[int]bool, label string) string {
	out := sc.BarsSVG([]int{bar}, []map[int]bool{marks}, []string{label})
	return out[0]
}

// BarsSVG is BarSVG for bars drawn side by side: the same height, their
// staves level, their syllables on one line.
func (sc Score) BarsSVG(bars []int, marks []map[int]bool, labels []string) []string {
	out := make([]string, len(bars))
	staves := make([]*staff, len(bars))
	texts := make([]string, len(bars))
	low, high, textY := math.Inf(1), math.Inf(-1), 0.0
	for i, b := range bars {
		staves[i], texts[i] = sc.barStaff(b, marks[i])
		if staves[i] == nil {
			continue
		}
		low, high = math.Min(low, staves[i].minY), math.Max(high, staves[i].maxY)
	}
	for i := range texts {
		if texts[i] != "" {
			textY = high + 7.5
		}
	}
	for i, s := range staves {
		if s == nil {
			continue
		}
		if texts[i] != "" {
			fmt.Fprintf(&s.b, `<g class="rr-lyrics" font-size="7" fill="currentColor" transform="translate(0 %.1f)">%s</g>`, textY, texts[i])
		}
		s.minY, s.maxY = low, high
		y0, y1 := s.extent(textY)
		out[i] = s.svgIn(labels[i], y0, y1)
	}
	return out
}

// barStaff draws bar, its syllables apart (placed under the staff by
// BarsSVG); nil for no such bar.
func (sc Score) barStaff(bar int, marks map[int]bool) (*staff, string) {
	if bar < 0 || bar >= len(sc.Sheet.Bars) {
		return nil, ""
	}
	b := sc.Sheet.Bars[bar]
	s := newStaff(sc.medianNote() < lowClef)
	s.clef(staffLeft + 1)
	// First where each event goes: its value's room, an accidental's,
	// half its syllables' width either side.
	type laid struct {
		ev     Event
		x      float64
		p      int
		alter  int
		acc    bool // an accidental written before it
		text   string
		textHW float64 // half the syllables' width
	}
	evs := make([]laid, len(b.Events))
	carried := map[int]int{} // a place's alteration, carried through the bar
	for i, ev := range b.Events {
		l := laid{ev: ev}
		if ev.Midi >= 0 {
			step, alter, oct := sc.Key.spell(ev.Midi + sc.Transpose)
			l.p, l.alter = pos(step, oct, s.low), alter
			if was, ok := carried[l.p]; (!ok && alter != 0) || (ok && was != alter) {
				l.acc = true
				carried[l.p] = alter
			}
			if sc.Lyrics != nil && !ev.TieIn && ev.Note >= 0 && ev.Note < len(sc.Lyrics) {
				if syls := sc.Lyrics[ev.Note].Syls; len(syls) > 0 {
					l.text = sylText(syls)
					l.textHW = float64(len([]rune(l.text))) * 1.8
				}
			}
		}
		evs[i] = l
	}
	x := staffLeft + clefW + 6
	for i := range evs {
		if evs[i].acc {
			x += 4.5
		}
		if i > 0 {
			// The syllables of this one and the one before clear each other.
			x = math.Max(x, evs[i-1].x+evs[i-1].textHW+evs[i].textHW+2.5)
		}
		x = math.Max(x, staffLeft+clefW+2+evs[i].textHW)
		evs[i].x = x
		x += 6 + float64(evs[i].ev.Len)*1.5
	}
	end := x + 2
	if n := len(evs); n > 0 {
		end = math.Max(end, evs[n-1].x+evs[n-1].textHW+2)
	}

	var texts strings.Builder
	for i, l := range evs {
		ev, x := l.ev, l.x
		if ev.Midi < 0 {
			s.rest(x+1, ev.Len)
			continue
		}
		cls := ""
		if marks[ev.Note] {
			cls = ` class="rr-hi"`
		}
		y := s.y(l.p)
		if l.acc {
			s.accidental(x, y, l.alter, l.alter == 0, cls)
		}
		s.ledgers(x, l.p)
		d := durations[ev.Len]
		base := ev.Len
		if d.dot {
			base = ev.Len * 2 / 3
		}
		s.head(x, y, base >= 8, base >= 16, cls)
		s.see(y - headRy)
		s.see(y + headRy)
		if base < 16 {
			flags := 0
			switch {
			case base <= 1:
				flags = 2
			case base <= 2:
				flags = 1
			}
			s.stemAndFlags(x, y, l.p, flags, cls)
		}
		if d.dot {
			dy := y
			if l.p%2 == 0 { // on a line: the dot in the space above
				dy -= sp / 2
			}
			fmt.Fprintf(&s.b, `<circle%s cx="%.1f" cy="%.1f" r="0.8" fill="currentColor"/>`, cls, x+headRx+2, dy)
		}
		// A tie from the note before (in this bar), or from the bar line;
		// one into the next bar, to the bar line.
		if ev.TieIn {
			x0 := staffLeft + clefW + 1
			if i > 0 && evs[i-1].ev.Note == ev.Note {
				x0 = evs[i-1].x + 1.5
			}
			s.tie(x0, x-1.5, y, l.p)
		}
		if ev.TieOut && ev.Pos+ev.Len >= b.Meter.Ticks() {
			s.tie(x+1.5, end-2, y, l.p)
		}
		if l.text != "" {
			fmt.Fprintf(&texts, `<text x="%.1f" text-anchor="middle"%s>%s</text>`, x, cls, html.EscapeString(l.text))
		}
	}
	s.width = end
	s.lines(staffLeft, s.width-1)
	return s, texts.String()
}

// tie draws a tie from x0 to x1 at a note's height, curved away from the
// stem.
func (s *staff) tie(x0, x1, y float64, p int) {
	if x1-x0 < 2 {
		return
	}
	// Stem up (under the middle line): the tie under the heads; down:
	// over them.
	y, dy := y+2.6, 3.0
	if p >= middleLine {
		y, dy = y-5.2, -3.0
	}
	fmt.Fprintf(&s.b, `<path fill="none" stroke="currentColor" stroke-width="0.8" d="M%.1f %.1f Q%.1f %.1f %.1f %.1f"/>`, x0, y, (x0+x1)/2, y+dy, x1, y)
	s.see(y + dy)
}

// sylText writes the syllables sung on one note: a word's syllables
// joined, a word going on ending in "-".
func sylText(syls []Syl) string {
	var b strings.Builder
	for i, sy := range syls {
		if i > 0 && sy.WordStart {
			b.WriteByte(' ')
		}
		b.WriteString(sy.Text)
	}
	if !syls[len(syls)-1].WordEnd {
		b.WriteString("-")
	}
	return strings.TrimSpace(b.String())
}
