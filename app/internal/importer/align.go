package importer

import (
	"fmt"
	"strings"
)

// AlignOwnLyrics writes a song's own lyrics (those of oldBody) into a
// chart built from guide, a fetched version of the song whose sections
// are the chart's own (a song map made from it, say): each of the
// song's lyric lines is matched by its words to the guide's lines and
// goes to the section of its match, walking forward through the
// sections so a chorus sung again lands in the next chorus. A line
// matching nothing goes where the line before it went (the first lines
// to where the first match goes). The words written are the song's
// own, in their order; the guide only says where. Nothing is dropped.
// ok is false, and chart is returned as it is, when the guide's
// sections aren't the chart's or nothing matches.
func AlignOwnLyrics(chart, guide, oldBody string) (string, []string, bool) {
	cd, gd := scanDoc(chart), scanDoc(guide)
	if len(cd.secs) == 0 || len(cd.secs) != len(gd.secs) {
		return chart, nil, false
	}
	for i := range cd.secs {
		if strings.TrimSpace(cd.secs[i].label) != strings.TrimSpace(gd.secs[i].label) {
			return chart, nil, false
		}
	}
	// The guide's lines by section, by their words.
	guideWords := make([][][]string, len(gd.secs))
	for i, sec := range gd.secs {
		for _, l := range sec.lines {
			if l.kind == lkLyric {
				if w := normalizeWords(sanitizeLyric(l.text)); len(w) > 0 {
					guideWords[i] = append(guideWords[i], w)
				}
			}
		}
	}
	// Lines are broken differently from one version to another: a match
	// is most of the shorter line's words found in the longer one.
	matches := func(s int, w []string) bool {
		for _, g := range guideWords[s] {
			if containment(w, g) >= 0.7 {
				return true
			}
		}
		return false
	}
	type ownLine struct {
		text  string
		start bool // a section or blank-line boundary before it
	}
	var own []ownLine
	for _, sec := range scanDoc(oldBody).secs {
		boundary := true
		for _, l := range sec.lines {
			switch l.kind {
			case lkLyric:
				if s := sanitizeLyric(l.text); s != "" {
					own = append(own, ownLine{s, boundary})
					boundary = false
				}
			case lkBlank:
				boundary = true
			}
		}
	}
	if len(own) == 0 {
		return chart, nil, false
	}
	// Each line's section: its match's, a few sections ahead at most
	// (one stray match far on would strand every line after it). A line
	// matching nothing goes with the line before it, unless it starts a
	// stanza of its own (a section or blank line before it in the old
	// chart) after a section already holding as many lines as the guide
	// gives it: then it opens the next section the guide has lyrics for
	// that has none yet (a verse sung with other words than the guide's).
	at := make([]int, len(own))
	filled := make([]int, len(gd.secs))
	cur, matched := -1, 0
	var pending []int // lines before the first section is known
	for i, l := range own {
		at[i] = -1
		w := normalizeWords(l.text)
		lo := max(cur, 0)
		for s := lo; len(w) > 0 && s < len(gd.secs) && s <= lo+3; s++ {
			if matches(s, w) {
				at[i] = s
				matched++
				break
			}
		}
		if at[i] < 0 && cur >= 0 {
			at[i] = cur
			if l.start && filled[cur] >= len(guideWords[cur]) {
				for s := cur + 1; s < len(gd.secs); s++ {
					if len(guideWords[s]) > 0 && filled[s] == 0 {
						at[i] = s
						break
					}
				}
			}
		}
		if at[i] < 0 {
			pending = append(pending, i)
			continue
		}
		if cur < 0 {
			for _, p := range pending {
				at[p] = at[i]
				filled[at[i]]++
			}
			pending = nil
		}
		cur = at[i]
		filled[cur]++
	}
	if matched == 0 {
		return chart, nil, false
	}
	bySec := make([][]string, len(cd.secs))
	for i, l := range own {
		bySec[at[i]] = append(bySec[at[i]], l.text)
	}
	gridOf := map[string][]srcLine{}
	for si := range cd.secs {
		lk := strings.ToLower(strings.TrimSpace(cd.secs[si].label))
		if gridOf[lk] == nil {
			for _, l := range cd.secs[si].lines {
				if l.kind == lkGrid {
					gridOf[lk] = append(gridOf[lk], l)
				}
			}
		}
	}
	nfilled := 0
	for si := range cd.secs {
		if len(bySec[si]) == 0 {
			continue
		}
		sec := &cd.secs[si]
		if !secHas(sec, lkGrid) {
			if g := gridOf[strings.ToLower(strings.TrimSpace(sec.label))]; g != nil {
				sec.lines = append(append([]srcLine{}, g...), sec.lines...)
			}
		}
		writeLyrics(sec, bySec[si])
		nfilled++
	}
	notes := []string{fmt.Sprintf("%d lines of the song's own lyrics into %d sections, %d placed by matching the guide's lines", len(own), nfilled, matched)}
	return cd.render(), notes, true
}

// containment is the share of the shorter word list found in the other,
// 0 for a list under two words (too little to tell).
func containment(a, b []string) float64 {
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(a) < 2 {
		return 0
	}
	in := map[string]bool{}
	for _, w := range b {
		in[w] = true
	}
	n := 0
	for _, w := range a {
		if in[w] {
			n++
		}
	}
	return float64(n) / float64(len(a))
}
