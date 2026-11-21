package httpapi

// The Jazz chart's form: how a song's section sequence is written the
// way an iReal Pro chart writes it — each section printed once with its
// rehearsal mark, and the order it is played in expressed with the
// chart's own road signs (repeat barlines with "3x" counts, first/second
// endings, segno + "D.S. al Coda" + coda, "D.C. al Fine"/"Fine") rather
// than by printing a section again every time it comes back.
//
// Everything here works on "cells": one printed bar each, in reading
// order, carrying the signs drawn on it. jzSimulate plays a list of
// cells the way a musician reads them (repeats, endings, D.S., coda,
// Fine), and that is the single definition of what a chart means: a
// section's grid is played as jzSimulate reads its own written cells
// (jzExpand, which realBarCount counts), and a compacted chart is only
// used when jzSimulate reads it back to exactly the song's played bars,
// bar for bar — otherwise every section is printed as written, in order.

import (
	"fmt"
	"strings"

	"app/internal/song"
)

// jzCell is one printed bar.
type jzCell struct {
	sec, bar int // the bar's source: section index (whose grid) and measure index

	open, close bool // repeat barlines: "|:" at the start, ":|" at the end
	times       int  // on a close: how many passes its bracket is played
	countText   int  // "3x" under a close (passes > 2, no endings to say it)
	endStart    bool // the first bar of an ending (volta)
	endLabels   []int
	endRunEnd   int // on endStart: index of the ending's last cell

	segno, coda  bool   // signs at the start of the bar
	toCoda, fine bool   // signs at the end of the bar
	jump         string // at the end of the bar: jzDSCoda, jzDSFine, jzDCCoda, jzDCFine
	withRepeats  bool   // the jump's text says the repeats are taken again

	// Layout: a block is a printed section (it starts a new line, with
	// its rehearsal mark); alignWith starts a new line padded to the
	// column of another cell (a later ending under the first one).
	blockStart bool
	unit       int // the form unit the block prints (-1: none)
	codaLine   bool
	alignWith  int // -1: none
}

var jzDebug = false

const (
	jzDSCoda = "D.S. al Coda"
	jzDSFine = "D.S. al Fine"
	jzDCCoda = "D.C. al Coda"
	jzDCFine = "D.C. al Fine"
)

func newCell(sec, bar int) jzCell { return jzCell{sec: sec, bar: bar, unit: -1, alignWith: -1} }

// jzSimulate reads cells the way a player does and returns the cell
// index of every bar played, in order. Repeats are taken again after a
// D.S./D.C. (the chart says "with repeats" when that matters).
func jzSimulate(cells []jzCell) []int {
	var out []int
	segno, coda := -1, -1
	for i, c := range cells {
		if c.segno && segno < 0 {
			segno = i
		}
		if c.coda && coda < 0 {
			coda = i
		}
	}
	pos, curOpen, pass := 0, -1, 1
	jumped, dsTaken := false, false
	for steps := 0; pos >= 0 && pos < len(cells) && steps < 100000; steps++ {
		c := &cells[pos]
		if c.open && !jumped {
			curOpen, pass = pos, 1
		}
		jumped = false
		if c.endStart && !jzHas(c.endLabels, pass) {
			pos = c.endRunEnd + 1
			continue
		}
		out = append(out, pos)
		if c.close && curOpen >= 0 && pass < c.times {
			pass++
			pos, jumped = curOpen, true
			continue
		}
		if c.fine && dsTaken {
			break
		}
		if c.toCoda && dsTaken && coda >= 0 {
			pos = coda
			continue
		}
		if c.jump != "" && !dsTaken {
			dsTaken = true
			pos, curOpen, pass = 0, -1, 1
			if strings.HasPrefix(c.jump, "D.S.") {
				pos = segno
			}
			continue
		}
		pos++
	}
	return out
}

func jzHas(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// ---- A section's written grid ---------------------------------------

// jzBracket is one "|: … :|" bracket of a written grid, with its endings.
type jzBracket struct {
	open, close int // first bar; last bar before any ending written after the close
	regionEnd   int // last bar of the bracket, endings included
	passes      int
	endings     []jzEnding
}

type jzEnding struct {
	labels     []int // the passes that take it
	start, end int
}

// jzVoltaPasses reads a volta marker ("1.", "1,2,3.", "(1)", "1-3.")
// into the passes it names.
func jzVoltaPasses(v string) []int {
	var out []int
	num, from := -1, -1
	flush := func() {
		if num < 0 {
			return
		}
		if from > 0 && from < num {
			for p := from; p <= num; p++ {
				out = append(out, p)
			}
		} else {
			out = append(out, num)
		}
		num, from = -1, -1
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			if num < 0 {
				num = 0
			}
			num = num*10 + int(r-'0')
		case r == '-' && num > 0:
			from, num = num, -1
		default:
			flush()
		}
	}
	flush()
	return out
}

// jzShape finds a grid's repeat brackets. It reads both ways this app's
// songs write endings: the usual one, the repeat sign closing the first
// ending ("|: a | (1.) b :| (2.) c |"), and the iReal export's, the sign
// closing the whole construct after the last ending ("|: (1,2,3.) a |
// (4.) b :|"). A ":|" with no "|:" repeats from the section's start (or
// the end of the previous bracket); a "|:" never closed is a repeat only
// when endings follow it.
func jzShape(ms []song.Measure) []jzBracket {
	n := len(ms)
	var out []jzBracket
	i, segStart := 0, 0
	for i < n {
		o, c := -1, -1
		for k := i; k < n; k++ {
			if ms[k].Open {
				o = k
				break
			}
		}
		for k := i; k < n; k++ {
			if ms[k].Close {
				c = k
				break
			}
		}
		var b jzBracket
		switch {
		case c >= 0 && (o < 0 || c < o):
			b = jzBracket{open: segStart, close: c}
		case o >= 0:
			next := n
			for k := o + 1; k < n; k++ {
				if ms[k].Open {
					next = k
					break
				}
			}
			c = -1
			for k := o; k < next; k++ {
				if ms[k].Close {
					c = k
					break
				}
			}
			if c < 0 {
				last := -1
				for k := o + 1; k < next; k++ {
					if ms[k].Volta != "" {
						last = k
					}
				}
				if last < 0 {
					i = o + 1
					continue
				}
				c = last - 1
			}
			b = jzBracket{open: o, close: c}
		default:
			return out
		}
		// Endings inside the bracket: each runs to the next one, or the close.
		for k := b.open; k <= b.close; k++ {
			if ms[k].Volta == "" {
				continue
			}
			end := b.close
			for j := k + 1; j <= b.close; j++ {
				if ms[j].Volta != "" {
					end = j - 1
					break
				}
			}
			b.endings = append(b.endings, jzEnding{labels: jzVoltaPasses(ms[k].Volta), start: k, end: end})
			k = end
		}
		// Endings written after the close: each runs through its own close,
		// or (the last one) on to the next sign or the grid's end.
		b.regionEnd = b.close
		for j := b.close + 1; j < n && ms[j].Volta != "" && !ms[j].Open; {
			end := j
			if !ms[j].Close {
				for k := j + 1; k < n && ms[k].Volta == "" && !ms[k].Open; k++ {
					end = k
					if ms[k].Close {
						break
					}
				}
			}
			b.endings = append(b.endings, jzEnding{labels: jzVoltaPasses(ms[j].Volta), start: j, end: end})
			b.regionEnd = end
			j = end + 1
		}
		maxLabel := 0
		for _, e := range b.endings {
			for _, l := range e.labels {
				if l > maxLabel {
					maxLabel = l
				}
			}
		}
		b.passes = maxLabel
		if rc := ms[b.open].RepeatCount; rc > b.passes {
			b.passes = rc
		}
		if b.passes < 2 {
			b.passes = 2
		}
		// One ending taken on every pass is no ending at all: "|: (1,2,3,4.)
		// B | A E :|" is a 4x repeat.
		if len(b.endings) == 1 {
			all := true
			for p := 1; p <= b.passes; p++ {
				if !jzHas(b.endings[0].labels, p) {
					all = false
				}
			}
			if all {
				b.endings = nil
			}
		}
		out = append(out, b)
		i = b.regionEnd + 1
		segStart = i
	}
	return out
}

// jzWrittenCells is a grid printed as written, its brackets drawn the
// iReal way: the repeat sign closes every ending that is played again
// ("1, 2." closes, the last "3." doesn't), whichever way the source
// placed it.
func jzWrittenCells(sec int, ms []song.Measure) []jzCell {
	cells := make([]jzCell, len(ms))
	for i := range ms {
		cells[i] = newCell(sec, i)
	}
	for _, b := range jzShape(ms) {
		cells[b.open].open = true
		if len(b.endings) == 0 {
			cells[b.close].close = true
			cells[b.close].times = b.passes
			if b.passes > 2 {
				cells[b.close].countText = b.passes
			}
			continue
		}
		// No ending for any pass but the last: the passes before it go back
		// at the end of the bars they share ("|: … | (4.) C :|").
		lastOnly := true
		for _, e := range b.endings {
			for _, l := range e.labels {
				if l < b.passes {
					lastOnly = false
				}
			}
		}
		if first := b.endings[0].start; lastOnly && first > b.open {
			cells[first-1].close = true
			cells[first-1].times = b.passes
		}
		for _, e := range b.endings {
			cells[e.start].endStart = true
			cells[e.start].endLabels = e.labels
			cells[e.start].endRunEnd = e.end
			minLabel := b.passes
			for _, l := range e.labels {
				if l < minLabel {
					minLabel = l
				}
			}
			if minLabel < b.passes {
				cells[e.end].close = true
				cells[e.end].times = b.passes
			}
		}
	}
	return cells
}

// jzExpand is the order a grid's written bars are played in.
func jzExpand(ms []song.Measure) []int {
	cells := jzWrittenCells(0, ms)
	played := jzSimulate(cells)
	out := make([]int, len(played))
	for i, p := range played {
		out[i] = cells[p].bar
	}
	return out
}

// ---- The song's form -------------------------------------------------

// jzOcc is one played section: its grid (its own, or the one it plays
// over), played reps times.
type jzOcc struct {
	sec   int // section index
	label string
	norm  string
	grid  int // section whose grid it plays
	reps  int
	class int
	bars  int // played bars: reps passes of the grid
}

// jzUnit is a run of identical consecutive occurrences.
type jzUnit struct {
	class int
	grid  int
	reps  int
	occs  []int // indices into the occurrence list
	n     int   // written bars
	label string
	norm  string
	// simple: no bracket of its own, played once — free to sit inside a
	// group repeat, an ending or a D.S. slice cut at a bar.
	simple bool
	// whole: the grid is one plain bracket over all its bars (no endings),
	// so playing the unit again just raises the bracket's count.
	whole       bool
	wholePasses int
	plain       bool // no bracket at all
}

// jzPiece is bars [from, to) of a unit's grid, written out copies times
// (a bracket unrolled so the unit can sit inside a bigger repeat).
type jzPiece struct{ u, from, to, copies int }

const (
	jzNodeUnit = iota
	jzNodeGroup
	jzNodePiece
)

type jzNode struct {
	kind      int
	u         int       // jzNodeUnit
	piece     jzPiece   // jzNodePiece
	common    []jzPiece // jzNodeGroup
	endings   [][]jzPiece
	endLabels [][]int
	passes    int
	markTail  bool // jzNodePiece cut after its first bars: still show its section's mark
}

// jzDS is the chart's one D.S./D.C. road map: play the nodes in order
// up to the end of node jump, go back to the segno (node segno, the top
// for a D.C.), play to the To Coda sign (after bar toBar of node toNode;
// toBar < 0 means the node's end) and jump to the coda (node coda), or
// stop there at Fine (coda < 0).
type jzDS struct {
	segno, jump   int
	toNode, toBar int
	coda          int
	dc            bool
	withRepeats   bool
}

type jzPlan struct {
	nodes []jzNode
	ds    *jzDS
	cost  float64
}

// Costs: printed bars, plus a little for each sign, so that of two
// charts as long as each other the plainer one wins, and a D.S. is only
// used when it saves more than a few bars.
const (
	jzCostUnitRepeat = 0.3
	jzCostGroup      = 0.5
	jzCostEndings    = 1.0
	// Whole sections as first and second endings (|: A [1. B :| [2. C):
	// legal, but a D.S. reads more plainly when it does as well.
	jzCostSectionEndings = 5.0
	jzCostDS             = 4.5
	jzCostCodaCut        = 0.5
	// A coda that prints again a section the chart already printed gains
	// little over printing the song in order.
	jzCostCodaRepeat = 3.0
)

// classes is the set of unit classes the nodes print.
func (f *jzForm) classes(nodes []jzNode) map[int]bool {
	out := map[int]bool{}
	for _, n := range nodes {
		switch n.kind {
		case jzNodeUnit:
			out[f.units[n.u].class] = true
		case jzNodePiece:
			out[f.units[n.piece.u].class] = true
		default:
			for _, p := range n.common {
				out[f.units[p.u].class] = true
			}
			for _, e := range n.endings {
				for _, p := range e {
					out[f.units[p.u].class] = true
				}
			}
		}
	}
	return out
}

// codaRepeats counts the nodes of a later part (a coda; the stretch
// between the segno's part and the D.S.) printing a section printed in
// an earlier one: a D.S. that still prints the section again is a detour.
func (f *jzForm) codaRepeats(first, coda []jzNode) float64 {
	seen := f.classes(first)
	n := 0
	for _, c := range coda {
		for cl := range f.classes([]jzNode{c}) {
			if seen[cl] {
				n++
				break
			}
		}
	}
	return float64(n) * jzCostCodaRepeat
}

type jzForm struct {
	units    []jzUnit
	measures map[int][]song.Measure    // each grid, by section index
	key      func(sec, bar int) string // a bar's content, to compare bars
	memo     map[[2]int]jzPlan
}

func (f *jzForm) bars(u int) int { return f.units[u].n }

func (f *jzForm) unitCost(u int) float64 {
	un := f.units[u]
	switch {
	case un.reps <= 1:
		return float64(un.n)
	case un.plain || un.whole:
		return float64(un.n) + jzCostUnitRepeat
	default:
		return float64(un.n * un.reps)
	}
}

func (f *jzForm) sameUnit(a, b int) bool {
	return f.units[a].class == f.units[b].class && f.units[a].reps == f.units[b].reps
}

// groupable: the unit can sit inside a group repeat — no bracket of its
// own, or one plain bracket over the whole grid, written out.
func (f *jzForm) groupable(u int) bool {
	un := f.units[u]
	return un.simple || (un.whole && un.reps == 1)
}

// copies is how many times a groupable unit's bars are written out.
func (f *jzForm) copies(u int) int {
	if un := f.units[u]; !un.simple && un.whole {
		return un.wholePasses
	}
	return 1
}

// prefix is how many leading bars two units' grids share.
func (f *jzForm) prefix(a, b int) int {
	ua, ub := f.units[a], f.units[b]
	k := 0
	for k < ua.n && k < ub.n && f.key(ua.grid, k) == f.key(ub.grid, k) {
		k++
	}
	return k
}

// compress is the cheapest way to print units [a, b) with repeats and
// endings only (no D.S.).
func (f *jzForm) compress(a, b int) jzPlan {
	if a >= b {
		return jzPlan{}
	}
	if p, ok := f.memo[[2]int{a, b}]; ok {
		return p
	}
	best := jzPlan{cost: -1}
	try := func(n jzNode, cost float64, next int) {
		rest := f.compress(next, b)
		total := cost + rest.cost
		if best.cost < 0 || total < best.cost-1e-9 {
			best = jzPlan{nodes: append([]jzNode{n}, rest.nodes...), cost: total}
		}
	}
	// The unit on its own.
	try(jzNode{kind: jzNodeUnit, u: a}, f.unitCost(a), a+1)
	// A group of g units played r times: |: A B :| (3x).
	for g := 2; a+2*g <= b; g++ {
		ok := true
		bars := 0
		for k := a; k < a+g; k++ {
			if !f.groupable(k) {
				ok = false
			}
			bars += f.units[k].n * f.copies(k)
		}
		if !ok {
			break
		}
		copies := 1
		for a+(copies+1)*g <= b {
			same := true
			for d := 0; d < g; d++ {
				if !f.sameUnit(a+d, a+copies*g+d) {
					same = false
					break
				}
			}
			if !same {
				break
			}
			copies++
		}
		common := make([]jzPiece, 0, g)
		for k := a; k < a+g; k++ {
			common = append(common, jzPiece{u: k, to: f.units[k].n, copies: f.copies(k)})
		}
		for r := 2; r <= copies; r++ {
			try(jzNode{kind: jzNodeGroup, common: common, passes: r}, float64(bars)+jzCostGroup, a+r*g)
		}
		// The same group, its last pass ending differently: |: A [1. B :|
		// [2. C. With B and C under one name sharing their first bars, the
		// endings are only the bars that differ.
		for r := 2; r <= copies+1; r++ {
			last := a + (r-1)*g + g - 1 // the unit that differs on the last pass
			if last >= b {
				break
			}
			ok := true
			for d := 0; d < g-1; d++ {
				if !f.sameUnit(a+d, a+(r-1)*g+d) {
					ok = false
				}
			}
			x, y := a+g-1, last
			if !ok || !f.units[x].simple || !f.units[y].simple || f.sameUnit(x, y) {
				continue
			}
			p := 0
			if f.units[x].norm == f.units[y].norm {
				p = f.prefix(x, y)
			}
			if p >= f.units[x].n || p >= f.units[y].n {
				continue
			}
			f.tryEndings(try, a, g, r, x, y, p)
		}
	}
	// One unit played again but ending differently, under one name:
	// |: V [1. … :| [2. … — the run of the same unit, then its variant.
	if a+1 < b {
		x, y := a, a+1
		ux, uy := f.units[x], f.units[y]
		if ux.plain && uy.simple && ux.norm == uy.norm && !f.sameUnit(x, y) {
			if p := f.prefix(x, y); p > 0 && p < ux.n && p < uy.n {
				f.tryEndings(try, a, 1, ux.reps+1, x, y, p)
			}
		}
	}
	if f.memo == nil {
		f.memo = map[[2]int]jzPlan{}
	}
	f.memo[[2]int{a, b}] = best
	return best
}

// tryEndings offers the group of g units starting at a, played r times,
// the last pass taking unit y in place of unit x, both sharing p bars.
func (f *jzForm) tryEndings(try func(jzNode, float64, int), a, g, r, x, y, p int) {
	var common []jzPiece
	bars := 0
	for k := a; k < a+g-1; k++ {
		common = append(common, jzPiece{u: k, to: f.units[k].n, copies: f.copies(k)})
		bars += f.units[k].n * f.copies(k)
	}
	if p > 0 {
		common = append(common, jzPiece{u: x, to: p})
	}
	if len(common) == 0 {
		return
	}
	labels := make([]int, 0, r-1)
	for i := 1; i < r; i++ {
		labels = append(labels, i)
	}
	n := jzNode{kind: jzNodeGroup, common: common, passes: r,
		endings:   [][]jzPiece{{{u: x, from: p, to: f.units[x].n}}, {{u: y, from: p, to: f.units[y].n}}},
		endLabels: [][]int{labels, {r}},
	}
	cost := float64(bars+f.units[x].n+f.units[y].n-p) + jzCostEndings
	if p == 0 {
		cost += jzCostSectionEndings - jzCostEndings
	}
	next := a + (r-1)*g + g
	if g == 1 {
		next = a + 2
	}
	try(n, cost, next)
}

// plan is the cheapest chart for the whole song: repeats and endings
// alone, or with one D.S./D.C. road map.
func (f *jzForm) plan() jzPlan {
	m := len(f.units)
	best := f.compress(0, m)
	concat := func(parts ...jzPlan) ([]jzNode, float64) {
		var nodes []jzNode
		cost := 0.0
		for _, p := range parts {
			nodes = append(nodes, p.nodes...)
			cost += p.cost
		}
		return nodes, cost
	}
	for s := 0; s < m; s++ {
		for L := 1; s+L < m; L++ {
			for e := s + L + 1; e+L <= m; e++ {
				match := true
				for d := 0; d < L; d++ {
					if !f.sameUnit(s+d, e+d) {
						match = false
						break
					}
				}
				if !match {
					continue
				}
				pre, slice := f.compress(0, s), f.compress(s, s+L)
				withRepeats := false
				for _, n := range slice.nodes {
					if n.kind == jzNodeGroup || f.units[n.u].reps > 1 || !f.units[n.u].plain {
						withRepeats = true
					}
				}
				// To Coda (or Fine) at the end of the slice.
				{
					gap, coda := f.compress(s+L, e), f.compress(e+L, m)
					nodes, cost := concat(pre, slice, gap)
					ds := &jzDS{segno: len(pre.nodes), jump: len(nodes) - 1, toNode: len(pre.nodes) + len(slice.nodes) - 1, toBar: -1, coda: -1, dc: s == 0, withRepeats: withRepeats}
					if len(coda.nodes) > 0 {
						ds.coda = len(nodes)
					}
					cost += coda.cost + jzCostDS + f.codaRepeats(nodes, coda.nodes) + f.codaRepeats(slice.nodes, gap.nodes) + f.codaRepeats(slice.nodes, pre.nodes)
					nodes = append(nodes, coda.nodes...)
					if cost < best.cost-1e-9 {
						best = jzPlan{nodes: nodes, ds: ds, cost: cost}
					}
				}
				// To Coda inside the first section after the slice: the last
				// time through, it starts like that section and ends its own way.
				if t := e + L; t < m {
					m0 := s + L
					if !f.units[m0].simple || !f.units[t].simple || f.sameUnit(m0, t) {
						continue
					}
					k := f.prefix(m0, t)
					if k == 0 || k >= f.units[m0].n || k >= f.units[t].n {
						continue
					}
					gapRest, codaRest := f.compress(m0+1, e), f.compress(t+1, m)
					nodes, cost := concat(pre, slice)
					toNode := len(nodes)
					nodes = append(nodes, jzNode{kind: jzNodePiece, piece: jzPiece{u: m0, to: f.units[m0].n}})
					nodes = append(nodes, gapRest.nodes...)
					jump := len(nodes) - 1
					coda := len(nodes)
					nodes = append(nodes, jzNode{kind: jzNodePiece, piece: jzPiece{u: t, from: k, to: f.units[t].n}, markTail: f.units[t].norm != f.units[m0].norm})
					cost += float64(f.units[m0].n) + gapRest.cost + float64(f.units[t].n-k) + codaRest.cost + jzCostDS + jzCostCodaCut + f.codaRepeats(nodes[:coda], codaRest.nodes) + f.codaRepeats(slice.nodes, gapRest.nodes) + f.codaRepeats(slice.nodes, pre.nodes)
					nodes = append(nodes, codaRest.nodes...)
					if cost < best.cost-1e-9 {
						best = jzPlan{nodes: nodes, ds: &jzDS{segno: len(pre.nodes), jump: jump, toNode: toNode, toBar: k - 1, coda: coda, dc: s == 0, withRepeats: withRepeats}, cost: cost}
					}
				}
			}
		}
	}
	return best
}

// ---- Printing a plan into cells ----------------------------------------

type jzEmitter struct {
	f     *jzForm
	cells []jzCell
}

func (em *jzEmitter) pieceCells(p jzPiece) (first, last int) {
	un := em.f.units[p.u]
	first = len(em.cells)
	copies := p.copies
	if copies < 1 {
		copies = 1
	}
	for k := 0; k < copies; k++ {
		for bar := p.from; bar < p.to; bar++ {
			c := newCell(un.grid, bar)
			if k == 0 && bar == p.from && p.from == 0 {
				c.blockStart, c.unit = true, p.u
			}
			em.cells = append(em.cells, c)
		}
	}
	return first, len(em.cells) - 1
}

// node prints one node and returns its first and last cell.
func (em *jzEmitter) node(n jzNode) (first, last int) {
	switch n.kind {
	case jzNodePiece:
		first, last = em.pieceCells(n.piece)
		if n.piece.from > 0 {
			em.cells[first].blockStart = true
			if n.markTail {
				em.cells[first].unit = n.piece.u
			}
		}
		return first, last
	case jzNodeUnit:
		un := em.f.units[n.u]
		if un.plain {
			first, last = em.pieceCells(jzPiece{u: n.u, to: un.n})
			if un.reps > 1 {
				em.cells[first].open = true
				em.cells[last].close, em.cells[last].times = true, un.reps
				if un.reps > 2 {
					em.cells[last].countText = un.reps
				}
			}
			return first, last
		}
		copies := 1
		if !un.whole {
			copies = un.reps
		}
		first = len(em.cells)
		for c := 0; c < copies; c++ {
			base := len(em.cells)
			for _, wc := range jzWrittenCells(un.grid, gridOf(em.f, n.u)) {
				if wc.endStart {
					wc.endRunEnd += base
				}
				em.cells = append(em.cells, wc)
			}
			em.cells[base].blockStart, em.cells[base].unit = true, n.u
			if un.whole && un.reps > 1 {
				lc := &em.cells[len(em.cells)-1]
				lc.times = un.wholePasses * un.reps
				lc.countText = lc.times
			}
		}
		return first, len(em.cells) - 1
	}
	// A group.
	first = len(em.cells)
	for _, p := range n.common {
		em.pieceCells(p)
	}
	em.cells[first].open = true
	if len(n.endings) == 0 {
		last = len(em.cells) - 1
		em.cells[last].close, em.cells[last].times = true, n.passes
		if n.passes > 2 {
			em.cells[last].countText = n.passes
		}
		return first, last
	}
	firstEnding := -1
	for i, ending := range n.endings {
		start := len(em.cells)
		for _, p := range ending {
			em.pieceCells(p)
		}
		end := len(em.cells) - 1
		em.cells[start].endStart = true
		em.cells[start].endLabels = n.endLabels[i]
		em.cells[start].endRunEnd = end
		if i == 0 {
			firstEnding = start
		} else if !em.cells[start].blockStart {
			em.cells[start].alignWith = firstEnding
		}
		if i < len(n.endings)-1 {
			em.cells[end].close, em.cells[end].times = true, n.passes
		}
	}
	return first, len(em.cells) - 1
}

func gridOf(f *jzForm, u int) []song.Measure { return f.measures[f.units[u].grid] }

func (em *jzEmitter) plan(p jzPlan) []jzCell {
	type span struct{ first, last int }
	spans := make([]span, len(p.nodes))
	for i, n := range p.nodes {
		a, b := em.node(n)
		spans[i] = span{a, b}
	}
	if ds := p.ds; ds != nil {
		if !ds.dc {
			em.cells[spans[ds.segno].first].segno = true
		}
		jc := &em.cells[spans[ds.jump].last]
		jc.withRepeats = ds.withRepeats
		at := spans[ds.toNode].last
		if ds.toBar >= 0 {
			at = spans[ds.toNode].first + ds.toBar
		}
		if ds.coda >= 0 {
			em.cells[at].toCoda = true
			cc := &em.cells[spans[ds.coda].first]
			cc.coda, cc.codaLine, cc.blockStart = true, true, true
			jc.jump = jzDSCoda
			if ds.dc {
				jc.jump = jzDCCoda
			}
		} else {
			em.cells[at].fine = true
			jc.jump = jzDSFine
			if ds.dc {
				jc.jump = jzDCFine
			}
		}
	}
	return em.cells
}

// ---- Building and checking a form --------------------------------------

// jzFormResult is a song's printed chart and, for every played bar, the
// cell it is read from.
type jzFormResult struct {
	cells   []jzCell
	played  []int // played bar -> cell
	units   []jzUnit
	occs    []jzOcc
	compact bool // false: the fallback, every section printed as written
}

// buildJazzForm finds the most compact faithful chart for the played
// occurrences. expected is the song's played bars as (section, bar)
// pairs, in order; key compares two bars' content.
func buildJazzForm(measures map[int][]song.Measure, occs []jzOcc, expected [][2]int, key func(sec, bar int) string) jzFormResult {
	f := &jzForm{key: key, measures: measures}
	for oi, o := range occs {
		if k := len(f.units) - 1; k >= 0 && f.units[k].class == o.class {
			f.units[k].reps += o.reps
			f.units[k].occs = append(f.units[k].occs, oi)
			continue
		}
		ms := measures[o.grid]
		shape := jzShape(ms)
		un := jzUnit{class: o.class, grid: o.grid, reps: o.reps, occs: []int{oi}, n: len(ms), label: o.label, norm: o.norm}
		un.plain = len(shape) == 0
		if len(shape) == 1 && shape[0].open == 0 && shape[0].regionEnd == len(ms)-1 && len(shape[0].endings) == 0 {
			un.whole, un.wholePasses = true, shape[0].passes
		}
		f.units = append(f.units, un)
	}
	for i := range f.units {
		f.units[i].simple = f.units[i].plain && f.units[i].reps == 1
	}
	check := func(cells []jzCell) ([]int, bool) {
		played := jzSimulate(cells)
		if len(played) != len(expected) {
			return nil, false
		}
		for i, ci := range played {
			if key(cells[ci].sec, cells[ci].bar) != key(expected[i][0], expected[i][1]) {
				return nil, false
			}
		}
		return played, true
	}
	if p := f.plan(); len(p.nodes) > 0 {
		em := &jzEmitter{f: f}
		cells := em.plan(p)
		if played, ok := check(cells); ok {
			return jzFormResult{cells: cells, played: played, units: f.units, occs: occs, compact: true}
		}
		if jzDebug {
			fmt.Println("  rejected:", jzFormResult{cells: cells, units: f.units}.describe(), len(jzSimulate(cells)), len(expected))
		}
	}
	// Fallback: every occurrence as written, in order.
	var cells []jzCell
	for _, o := range occs {
		for r := 0; r < o.reps; r++ {
			base := len(cells)
			for _, wc := range jzWrittenCells(o.grid, measures[o.grid]) {
				if wc.endStart {
					wc.endRunEnd += base
				}
				cells = append(cells, wc)
			}
			if len(cells) > base {
				cells[base].blockStart = true
				cells[base].unit = jzUnitOfOcc(f.units, o)
			}
		}
	}
	played, _ := check(cells)
	return jzFormResult{cells: cells, played: played, units: f.units, occs: occs}
}

func jzUnitOfOcc(units []jzUnit, o jzOcc) int {
	for i, u := range units {
		if u.class == o.class {
			return i
		}
	}
	return -1
}

// describe is a one-line, lyrics-free account of a chart's form, for
// tests and logs: blocks with their units, signs, endings.
func (r jzFormResult) describe() string {
	var b strings.Builder
	for i, c := range r.cells {
		if c.blockStart {
			if i > 0 {
				b.WriteString(" ")
			}
			if c.coda {
				b.WriteString("Φ")
			}
			b.WriteString("[")
			if c.unit >= 0 {
				b.WriteString(r.units[c.unit].label)
			}
			b.WriteString("]")
		}
		if c.segno {
			b.WriteString("§")
		}
		if c.open {
			b.WriteString("|:")
		}
		if c.endStart {
			fmt.Fprintf(&b, "(%s)", jzLabelText(c.endLabels))
		}
		b.WriteString(".")
		if c.toCoda {
			b.WriteString("→Φ")
		}
		if c.close {
			b.WriteString(":|")
			if c.countText > 0 {
				fmt.Fprintf(&b, "%dx", c.countText)
			}
		}
		if c.fine {
			b.WriteString("Fine")
		}
		if c.jump != "" {
			b.WriteString(" " + c.jump)
		}
	}
	return b.String()
}

func jzLabelText(labels []int) string {
	parts := make([]string, len(labels))
	for i, l := range labels {
		parts[i] = fmt.Sprint(l)
	}
	return strings.Join(parts, ", ") + "."
}
