package song

// PlayOrder is the order a grid's written measures are played in:
// indices into ms, repeats taken with their counts, first and second
// endings read both ways this app's songs write them — the usual one,
// the repeat sign closing the first ending ("|: a | (1.) b :| (2.) c |"),
// and the iReal export's, the sign closing the whole construct after the
// last ending ("|: (1,2,3.) a | (4.) b :|"). An ending runs over every
// bar up to the next ending or the close, not only the bar it's written
// on; "1-3." is passes 1, 2 and 3; a ":|" with no "|:" repeats from the
// grid's start (or the end of the previous bracket); a "|:" never closed
// is a repeat only when endings follow it.
//
// It is the Jazz chart's reading of a section's grid (httpapi's
// jzExpand, which the song's bar count follows), so the chart laid on a
// recording (chartalign) numbers its bars as the player does.
//
// limit stops it after that many bars (a "|:2000000" mustn't be played
// out to be counted); limit <= 0 is no limit but maxPlaySteps.
func PlayOrder(ms []Measure, limit int) []int {
	cells := playCells(ms)
	var out []int
	pos, curOpen, pass := 0, -1, 1
	jumped := false
	for steps := 0; pos >= 0 && pos < len(cells) && steps < maxPlaySteps; steps++ {
		c := &cells[pos]
		if c.open && !jumped {
			curOpen, pass = pos, 1
		}
		jumped = false
		if c.endStart && !hasPass(c.endLabels, pass) {
			pos = c.endRunEnd + 1
			continue
		}
		out = append(out, pos)
		if limit > 0 && len(out) >= limit {
			break
		}
		if c.close && curOpen >= 0 && pass < c.times {
			pass++
			pos, jumped = curOpen, true
			continue
		}
		pos++
	}
	return out
}

// maxPlaySteps bounds PlayOrder without a limit, as the Jazz chart's
// reader bounds itself.
const maxPlaySteps = 100000

type playCell struct {
	open, close bool
	times       int // on a close: how many passes its bracket is played
	endStart    bool
	endLabels   []int
	endRunEnd   int
}

type playBracket struct {
	open, close int // first bar; last bar before any ending written after the close
	regionEnd   int
	passes      int
	endings     []playEnding
}

type playEnding struct {
	labels     []int
	start, end int
}

// VoltaPasses reads a volta marker ("1.", "1,2,3.", "(1)", "1-3.") into
// the passes it names.
func VoltaPasses(v string) []int {
	var out []int
	num, from := -1, -1
	flush := func() {
		if num < 0 {
			return
		}
		if from > 0 && from < num {
			// A range is a few passes: "1-200000000." mustn't list them all.
			for p := from; p <= min(num, from+maxVoltaRange); p++ {
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
			if num < 1e8 {
				num = num*10 + int(r-'0')
			}
		case r == '-' && num > 0:
			from, num = num, -1
		default:
			flush()
		}
	}
	flush()
	return out
}

const maxVoltaRange = 64

func hasPass(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// playShape finds a grid's repeat brackets and their endings.
func playShape(ms []Measure) []playBracket {
	n := len(ms)
	var out []playBracket
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
		var b playBracket
		switch {
		case c >= 0 && (o < 0 || c < o):
			b = playBracket{open: segStart, close: c}
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
			b = playBracket{open: o, close: c}
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
			b.endings = append(b.endings, playEnding{labels: VoltaPasses(ms[k].Volta), start: k, end: end})
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
			b.endings = append(b.endings, playEnding{labels: VoltaPasses(ms[j].Volta), start: j, end: end})
			b.regionEnd = end
			j = end + 1
		}
		maxLabel := 0
		for _, e := range b.endings {
			for _, l := range e.labels {
				maxLabel = max(maxLabel, l)
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
			for p := 1; p <= b.passes && all; p++ {
				all = hasPass(b.endings[0].labels, p)
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

// playCells marks the repeat signs as they're read: the repeat sign
// closes every ending that is played again ("1, 2." close, the last "3."
// doesn't), whichever way the source placed it.
func playCells(ms []Measure) []playCell {
	cells := make([]playCell, len(ms))
	for _, b := range playShape(ms) {
		cells[b.open].open = true
		if len(b.endings) == 0 {
			cells[b.close].close = true
			cells[b.close].times = b.passes
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
				minLabel = min(minLabel, l)
			}
			if minLabel < b.passes {
				cells[e.end].close = true
				cells[e.end].times = b.passes
			}
		}
	}
	return cells
}
