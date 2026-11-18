package importer

import (
	"fmt"
	"strings"
)

// WithoutLyrics is body with every lyric line left out: header,
// sections, grids, scores and directives as they are.
func WithoutLyrics(body string) string {
	doc := scanDoc(body)
	for si := range doc.secs {
		sec := &doc.secs[si]
		kept := sec.lines[:0]
		for _, l := range sec.lines {
			if l.kind != lkLyric {
				kept = append(kept, l)
			}
		}
		sec.lines = kept
	}
	return collapseBlankRuns(doc.render())
}

// ResplitOwn moves a song's own lyrics into the right sections where an
// import ran two parts together, and changes nothing else: no line is
// dropped, rewritten or reordered within its part. The chorus is what
// the song sings more than once (the first chorus section's leading
// lines that come back elsewhere). A chorus section that goes on past
// its chorus with something else (a verse caught at its end) and a
// section of another kind holding the chorus are split, a chorus sung
// twice in its own section is not; the part that doesn't
// belong moves to the next empty section of its kind after it (a verse
// part to a verse, a chorus to a chorus), which takes the grid it used
// to inherit if it had none. With no such section, the part stays where
// it is. Notes say what moved where, without quoting the lyrics.
func ResplitOwn(body string) (string, []Placement, []string) {
	doc := scanDoc(body)
	key := func(s string) string { return strings.Join(normalizeWords(sanitizeLyric(s)), " ") }

	// The chorus: the first chorus section's leading lines sung again.
	count := map[string]int{}
	for _, sec := range doc.secs {
		for _, l := range sec.lines {
			if l.kind == lkLyric {
				count[key(l.text)]++
			}
		}
	}
	var chorus []string
	for _, sec := range doc.secs {
		if SectionKind(sec.label) != "chorus" {
			continue
		}
		for _, l := range sec.lines {
			if l.kind != lkLyric {
				continue
			}
			if k := key(l.text); k != "" && count[k] > 1 {
				chorus = append(chorus, k)
				continue
			}
			break
		}
		if len(chorus) > 0 {
			break
		}
	}
	isChorus := map[string]int{}
	for i, k := range chorus {
		if _, ok := isChorus[k]; !ok {
			isChorus[k] = i
		}
	}

	type part struct {
		lines  []srcLine
		chorus bool
	}
	// A section's lyrics in parts: runs of chorus lines (a run starts
	// over when the chorus does) and runs of other lines.
	parts := func(sec *srcSection) []part {
		var out []part
		last := -1
		for _, l := range sec.lines {
			if l.kind != lkLyric {
				continue
			}
			j, ch := isChorus[key(l.text)]
			n := len(out)
			if n == 0 || out[n-1].chorus != ch || (ch && j == 0 && last >= 0) {
				out = append(out, part{chorus: ch})
			}
			out[len(out)-1].lines = append(out[len(out)-1].lines, l)
			if ch {
				last = j
			} else {
				last = -1
			}
		}
		return out
	}

	hasLyrics := func(sec *srcSection) bool { return secHas(sec, lkLyric) }
	gridOf := map[string][]srcLine{}
	for si := range doc.secs {
		sec := &doc.secs[si]
		lk := strings.ToLower(strings.TrimSpace(sec.label))
		if gridOf[lk] == nil {
			for _, l := range sec.lines {
				if l.kind == lkGrid {
					gridOf[lk] = append(gridOf[lk], l)
				}
			}
		}
	}
	taken := map[int]bool{}
	received := map[int]bool{}
	var notes []string
	// target is the next section after from of the wanted kind with no
	// lyrics, not already given a part.
	target := func(from int, wantChorus bool) int {
		for i := from + 1; i < len(doc.secs); i++ {
			k := SectionKind(doc.secs[i].label)
			if (wantChorus && k == "chorus") || (!wantChorus && k == "verse") {
				if !hasLyrics(&doc.secs[i]) && !taken[i] && !doc.secs[i].repeat {
					return i
				}
			}
		}
		return -1
	}
	for si := range doc.secs {
		sec := &doc.secs[si]
		if received[si] || len(chorus) == 0 {
			continue
		}
		ps := parts(sec)
		if len(ps) < 2 {
			continue
		}
		kind := SectionKind(sec.label)
		// What stays: in a chorus section its first part and its chorus
		// runs (a chorus sung twice in its section is still the chorus);
		// elsewhere the parts that aren't the chorus.
		stay := map[int]bool{}
		if kind == "chorus" {
			stay[0] = true
			for i, p := range ps {
				if p.chorus {
					stay[i] = true
				}
			}
		} else {
			for i, p := range ps {
				if !p.chorus {
					stay[i] = true
				}
			}
			if len(stay) == 0 {
				continue
			}
		}
		var keep []srcLine
		for i, p := range ps {
			if stay[i] {
				keep = append(keep, p.lines...)
				continue
			}
			t := target(si, p.chorus)
			if t < 0 {
				keep = append(keep, p.lines...)
				continue
			}
			taken[t], received[t] = true, true
			dst := &doc.secs[t]
			if !secHas(dst, lkGrid) {
				if g := gridOf[strings.ToLower(strings.TrimSpace(dst.label))]; g != nil {
					dst.lines = append(append([]srcLine{}, g...), dst.lines...)
				}
			}
			var texts []string
			for _, l := range p.lines {
				texts = append(texts, l.text)
			}
			writeLyrics(dst, texts)
			what := "verse"
			if p.chorus {
				what = "chorus"
			}
			notes = append(notes, fmt.Sprintf("%s: %d lines (a %s) moved to %s", nameOf(doc, si), len(p.lines), what, nameOf(doc, t)))
		}
		if len(keep) != countLyrics(sec) {
			var texts []string
			for _, l := range keep {
				texts = append(texts, l.text)
			}
			writeLyrics(sec, texts)
		}
	}
	var placements []Placement
	for i := range doc.secs {
		p := Placement{Section: strings.TrimSpace(doc.secs[i].label), Stanza: -1}
		switch {
		case received[i]:
			p.Status = PlacePlaced
		case hasLyrics(&doc.secs[i]):
			p.Status = PlaceKept
		case doc.secs[i].repeat || (SectionKind(doc.secs[i].label) == "chorus" && len(chorus) > 0):
			p.Status = PlaceReused
		case instrumentalKinds[SectionKind(doc.secs[i].label)]:
			p.Status = PlaceInstrumental
		default:
			p.Status = PlaceMissing
		}
		placements = append(placements, p)
	}
	if len(notes) == 0 {
		notes = append(notes, "nothing to move: each section's lyrics are one part")
	}
	return doc.render(), placements, notes
}

func countLyrics(sec *srcSection) int {
	n := 0
	for _, l := range sec.lines {
		if l.kind == lkLyric {
			n++
		}
	}
	return n
}

// nameOf is a section's label, numbered when the song has several of it
// ("Verse 2").
func nameOf(doc srcDoc, i int) string {
	label := strings.TrimSpace(doc.secs[i].label)
	n, nth := 0, 0
	for j := range doc.secs {
		if strings.TrimSpace(doc.secs[j].label) == label {
			n++
			if j <= i {
				nth = n
			}
		}
	}
	if n > 1 {
		return fmt.Sprintf("%s %d", label, nth)
	}
	return label
}
