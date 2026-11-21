package pdfnative

// bar_rows.go: how many bars each printed row of a section holds — the Go
// twin of the web Song view's static/js/bar-rows.js, so that the PDF and
// the screen print the same rows. Both are checked against one table of
// cases, webassets/jstest/bar-rows-cases.json (TestBarRowsCases here,
// bar-rows.test.mjs there): a change to one that the other doesn't make
// fails a test.
//
// The lead-sheet convention: rows of 4-bar phrases (perRow), aligned from
// the section's first bar. A pickup bar rides on the first row on top of
// its count. What's left over at the end:
//   - 1 bar joins the row before it (5 → one row of 5, 9 → 4 + 5), as long
//     as that row can still hold it (fit; paper always can);
//   - 2 or 3 bars make a short last row (10 → 4 + 4 + 2) whose bars are as
//     wide as a full row's.
// A section whose author wrote "bars:" keeps the author's rows, each one
// split the same way only when it holds more than perRow; a lone bar such
// a split leaves where it can't join its row shares the row before it
// instead (bars: 5 where 5 don't fit: 3 + 2, not 4 + 1).

// barRow is one printed row: its bar count (a pickup included) and how
// many bars wide its regular bars are drawn (a short row's bars keep a
// full row's width).
type barRow struct {
	Bars int `json:"bars"`
	Cap  int `json:"cap"`
}

// barRowOpts: perRow bars to a row; the section opens with the song's
// pickup; the author wrote "bars:"; fit / fitFirst, the most regular bars
// a row / the first row can hold (0: no limit, as on paper).
type barRowOpts struct {
	PerRow   int  `json:"perRow"`
	Pickup   bool `json:"pickup"`
	Explicit bool `json:"explicit"`
	Fit      int  `json:"fit"`
	FitFirst int  `json:"fitFirst"`
}

const noFit = int(^uint(0) >> 1)

// rowSizes is the bar count of each row for n bars, perRow to a row (see
// bar-rows.js' sizes).
func rowSizes(n, perRow, fitFirst, fit int, balance bool) []int {
	p := max(1, perRow)
	var out []int
	for rest := n; rest > 0; rest -= p {
		out = append(out, min(p, rest))
	}
	k := len(out)
	if k > 1 && p > 1 && out[k-1] == 1 {
		room := fit
		if k == 2 {
			room = fitFirst
		}
		switch {
		case p+1 <= room:
			out[k-2]++
			out = out[:k-1]
		case balance && out[k-2] >= 3:
			out[k-2]--
			out[k-1] = 2
		}
	}
	return out
}

// planBarRows lays out one section's rows (see bar-rows.js' plan): rows
// are the bar counts of the section's rows as grouped before (the
// author's "bars:" rows, the pickup counted in the first).
func planBarRows(rows []int, o barRowOpts) []barRow {
	var counts []int
	for _, r := range rows {
		if r > 0 {
			counts = append(counts, r)
		}
	}
	if len(counts) == 0 {
		return nil
	}
	p := max(1, o.PerRow)
	pickup := 0
	if o.Pickup {
		pickup = 1
	}
	fit := o.Fit
	if fit <= 0 {
		fit = noFit
	}
	fitFirst := o.FitFirst
	if fitFirst <= 0 {
		fitFirst = fit
	}
	var out []barRow
	if !o.Explicit {
		n := -pickup
		for _, c := range counts {
			n += c
		}
		s := rowSizes(n, p, fitFirst, fit, false)
		if len(s) == 0 {
			return []barRow{{Bars: pickup, Cap: 1}}
		}
		for i, b := range s {
			r := barRow{Bars: b, Cap: max(b, p)}
			if i == 0 {
				r.Bars += pickup
			}
			out = append(out, r)
		}
		return out
	}
	pe := p
	longest := 0
	for _, c := range counts {
		longest = max(longest, c)
	}
	pe = min(pe, longest)
	for i, r := range counts {
		ff := fit
		if i == 0 {
			ff = fitFirst
			if ff < noFit {
				ff += pickup
			}
		}
		for j, b := range rowSizes(r, pe, ff, fit, true) {
			pk := 0
			if i == 0 && j == 0 {
				pk = pickup
			}
			c := pe
			if b >= pe {
				c = max(1, b-pk)
			}
			out = append(out, barRow{Bars: b, Cap: c})
		}
	}
	return out
}

// sectionBarRows is the PDF's rows for a section of n bars: the author's
// "bars:" rows when it has some, else phrases; pickup when it opens with
// the song's pickup bar (tab.StartsWithPickup).
func sectionBarRows(barsPerLine []int, n int, pickup bool) []barRow {
	if len(barsPerLine) == 0 {
		return planBarRows([]int{n}, barRowOpts{PerRow: phraseBars, Pickup: pickup})
	}
	var rows []int
	for start, li := 0, 0; start < n; li++ {
		want := barsPerLine[len(barsPerLine)-1]
		if li < len(barsPerLine) {
			want = barsPerLine[li]
		}
		k := min(max(want, 1), n-start)
		rows = append(rows, k)
		start += k
	}
	return planBarRows(rows, barRowOpts{PerRow: phraseBars, Pickup: pickup, Explicit: true})
}
