package importer

import (
	"regexp"
	"strings"
)

// SongMap turns a chart into a song map: its header, its sections in
// order and their grids, scores and directives, with every lyric line
// left out (and any free text above the first section, which in a
// fetched chart is often a lyric excerpt). A section that held only
// lyrics stays, bare: its chords carry on, as in a band's song map.
// The "map:" header line lists the sections in order. Lyrics are added
// one song at a time with the wizard's lyrics step.
func SongMap(text string) string {
	doc := scanDoc(text)
	var head []string
	for _, l := range doc.head {
		if t := strings.TrimSpace(l); t == "" || headerLine.MatchString(t) {
			head = append(head, l)
		}
	}
	doc.head = head
	doc.secs = dropLeadingEmpty(doc.secs)
	var labels []string
	for si := range doc.secs {
		sec := &doc.secs[si]
		kept := sec.lines[:0]
		for _, l := range sec.lines {
			if l.kind != lkLyric {
				kept = append(kept, l)
			}
		}
		sec.lines = kept
		labels = append(labels, strings.TrimSpace(sec.label))
	}
	out := doc.render()
	if len(labels) > 0 {
		out = setMapHeader(out, strings.Join(labels, " · "))
	}
	return collapseBlankRuns(out)
}

var headerLine = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\[[^\]]*\])?:`)

// setMapHeader writes the "map:" line, replacing one already there, at
// the end of the header.
func setMapHeader(text, value string) string {
	lines := strings.Split(text, "\n")
	end := 0
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			break
		}
		if strings.HasPrefix(t, "map:") {
			lines[i] = "map: " + value
			return strings.Join(lines, "\n")
		}
		if headerLine.MatchString(t) {
			end = i + 1
		}
	}
	out := append([]string{}, lines[:end]...)
	out = append(out, "map: "+value)
	return strings.Join(append(out, lines[end:]...), "\n")
}

func collapseBlankRuns(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimLeft(s, "\n")
}

// dropLeadingEmpty leaves out the sections before the first one with
// anything in it (a grid, lyrics, a score, a directive): an empty label
// at the very top (a fetched chart's "[Verse]" before its Intro) repeats
// nothing and carries no chords on. Bare sections after real ones stay:
// there they mean "again" or "the chords carry on".
func dropLeadingEmpty(secs []srcSection) []srcSection {
	for i, sec := range secs {
		for _, l := range sec.lines {
			if l.kind != lkBlank && l.kind != lkComment {
				return secs[i:]
			}
		}
	}
	return secs
}

// TidyMap is SongMap's structural cleanup for a song already saved as a
// song map: the empty sections at the top left out, and its "map:" line
// redone. Lyrics stay. It reports how many sections went.
func TidyMap(body string) (string, int) {
	doc := scanDoc(body)
	before := len(doc.secs)
	doc.secs = dropLeadingEmpty(doc.secs)
	gone := before - len(doc.secs)
	if gone == 0 {
		return body, 0
	}
	var labels []string
	for _, sec := range doc.secs {
		labels = append(labels, strings.TrimSpace(sec.label))
	}
	return collapseBlankRuns(setMapHeader(doc.render(), strings.Join(labels, " · "))), gone
}
