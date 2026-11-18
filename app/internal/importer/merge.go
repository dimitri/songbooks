package importer

import (
	"fmt"
	"strconv"
	"strings"

	"app/internal/chordsym"
	"app/internal/song"
)

// mergeHeaderKeys are the header fields a draft may fill in: only where
// the song has none, or an empty one.
var mergeHeaderKeys = []string{"title", "artist", "capo", "key", "tempo", "time"}

// MergeDraft integrates a fetched draft into an existing song, keeping the song's own structure.
//
// The song's header gains the draft's title, artist, capo, key, tempo
// and time only where it has none (or an empty one); its other header
// lines (map:, flats:, …) and the text before its first section stay as
// they are. A song with no sections yet (empty, or one placeholder
// section with nothing in it) takes the draft's sections (ReplaceDraft).
//
// Otherwise every section of the song stays, in its order, with its
// label, grid and lyric lines; nothing is added or removed. Draft
// sections are matched to the song's by kind (SectionKind; a label of no
// known kind by its words): the n-th verse to the n-th verse, every
// other kind to its kind (all choruses are the draft's first chorus).
//
//   - Grids: a kind's draft grid goes into the song's FIRST section of
//     that kind, only when that one has no grid. A later section of a
//     kind whose first one has a grid, or gets one, stays bare: in a
//     song map that's how it repeats the first one's chords. A bare Solo
//     never gets one (the app reads it as a solo over the changes), nor
//     does a "(repeat)" section.
//   - Lyrics: the draft's sections become stanzas (their label, their
//     lyric lines without inline chords) placed with PlaceLyrics, keeping
//     every section's existing lyrics: each verse gets its own stanza, a
//     chorus reuses the chorus's, a bare repeat of an earlier chorus gets
//     nothing (the viewer repeats it).
//
// The notes say what was added where and which draft sections found no
// section to go into; they never quote lyrics. Merging the same draft
// again changes nothing.
func MergeDraft(existing, draft string) (string, []string) {
	ex := scanDoc(existing)
	if !hasRealSections(ex) {
		text, notes := ReplaceDraft(existing, draft)
		return text, append([]string{"The song had no sections yet: took the draft's"}, notes[1:]...)
	}
	dr := scanDoc(draft)
	var notes []string

	head, added := mergeHead(ex.head, song.Parse(existing), song.Parse(draft))
	ex.head = head
	if len(added) > 0 {
		notes = append(notes, "Header: "+strings.Join(added, ", ")+" from the draft")
	}
	notes = append(notes, keyNote(song.Parse(existing), song.Parse(draft))...)

	names := displayNames(ex.secs)

	// Each song section's match key, and the draft's sections by key.
	exKeys := make([]string, len(ex.secs))
	exCount := map[string]int{}
	for i, s := range ex.secs {
		exKeys[i] = matchKey(s.label)
		exCount[exKeys[i]]++
	}
	type draftSec struct {
		label string
		key   string
		occ   int // its occurrence among the draft's sections of its key
		grid  []string
		lyric []string
		home  bool
	}
	var drs []draftSec
	drOcc := map[string]int{}
	for _, s := range dr.secs {
		d := draftSec{label: strings.TrimSpace(s.label), key: matchKey(s.label)}
		d.occ = drOcc[d.key]
		drOcc[d.key]++
		for _, l := range s.lines {
			switch l.kind {
			case lkGrid:
				t := strings.TrimSpace(l.text)
				if strings.HasPrefix(t, "|") {
					t = "grid: " + t
				}
				d.grid = append(d.grid, t)
			case lkLyric:
				if c := cleanStanzaLine(l.text); c != "" {
					d.lyric = append(d.lyric, c)
				}
			}
		}
		switch {
		case d.key == "":
		case d.key == "verse":
			d.home = d.occ < exCount["verse"]
		default:
			d.home = exCount[d.key] > 0
		}
		drs = append(drs, d)
	}

	// Grids: a kind's first section, when it has none.
	firstDone := map[string]bool{}
	for i := range ex.secs {
		sec := &ex.secs[i]
		k := exKeys[i]
		if k == "" || sec.repeat || firstDone[k] {
			continue
		}
		firstDone[k] = true
		if secHas(sec, lkGrid) || secHas(sec, lkScore) || hasGP(sec) {
			continue
		}
		if k == "solo" && !secHas(sec, lkLyric) {
			continue
		}
		// The draft's matching (first) section of this kind, else its
		// first one of this kind with a grid.
		var grid []string
		for _, d := range drs {
			if d.key == k && len(d.grid) > 0 {
				grid = d.grid
				break
			}
		}
		if len(grid) == 0 {
			continue
		}
		insertGrid(sec, grid)
		notes = append(notes, names[i]+": grid from the draft")
	}

	// Lyrics: the draft's sections as stanzas, labels kept.
	var stanzas []Stanza
	var stanzaOf []int // stanza -> draft section
	for di, d := range drs {
		if len(d.lyric) > 0 {
			stanzas = append(stanzas, Stanza{Label: d.label, Lines: d.lyric})
			stanzaOf = append(stanzaOf, di)
		}
	}
	text := ex.render()
	var unused []int
	if len(stanzas) > 0 {
		before := ex
		text, _, unused = PlaceLyrics(text, stanzas, false)
		after := scanDoc(text)
		for i := range after.secs {
			if i < len(before.secs) && !secHas(&before.secs[i], lkLyric) && secHas(&after.secs[i], lkLyric) {
				notes = append(notes, names[i]+": lyrics added")
			}
		}
	}

	// Draft sections with something in them and nowhere to go: neither
	// their grid nor their lyrics went in, nor those of another draft
	// section with their label (a draft's second chorus repeats the
	// first, whose lyrics every chorus already has).
	isUnused := map[int]bool{}
	for _, si := range unused {
		isUnused[si] = true
	}
	lyricsIn := map[int]bool{} // draft section -> its lyrics went in
	labelUsed := map[string]bool{}
	for si, di := range stanzaOf {
		if !isUnused[si] {
			lyricsIn[di] = true
			labelUsed[drs[di].label] = true
		}
	}
	for _, d := range drs {
		if d.home {
			labelUsed[d.label] = true
		}
	}
	reported := map[string]bool{}
	for di, d := range drs {
		if d.home || lyricsIn[di] || labelUsed[d.label] || (len(d.grid) == 0 && len(d.lyric) == 0) || reported[d.label] {
			continue
		}
		reported[d.label] = true
		if len(d.lyric) > 0 {
			notes = append(notes, "The draft’s “"+orUnnamed(d.label)+"”: no section left for it here, its lyrics not placed")
		} else {
			notes = append(notes, "The draft’s “"+orUnnamed(d.label)+"”: no matching section here")
		}
	}
	return text, notes
}

// ReplaceDraft is the draft in place of the song's sections: the song's
// header zone is kept as it is (map:, text before the first section and
// all), filled in from the draft's title, artist, capo, key, tempo and
// time where it has none, and with the draft's chord diagrams when it
// has none. The first note says what was done.
func ReplaceDraft(existing, draft string) (string, []string) {
	ex := scanDoc(existing)
	dr := scanDoc(draft)
	exSong, drSong := song.Parse(existing), song.Parse(draft)
	notes := []string{fmt.Sprintf("Sections replaced by the draft's (%d)", len(dr.secs))}
	head, added := mergeHead(ex.head, exSong, drSong)
	if len(added) > 0 {
		notes = append(notes, "Header: "+strings.Join(added, ", ")+" from the draft")
	}
	if len(exSong.Chords) == 0 && len(exSong.OtherChords) == 0 {
		var diagrams []string
		for _, h := range dr.head {
			if t := strings.TrimSpace(h); strings.HasPrefix(t, "chord:") || strings.HasPrefix(t, "chord[") {
				diagrams = append(diagrams, t)
			}
		}
		if len(diagrams) > 0 {
			head = insertHeadLines(head, diagrams)
			notes = append(notes, fmt.Sprintf("Header: %d chord diagrams from the draft", len(diagrams)))
		}
	}
	notes = append(notes, keyNote(exSong, drSong)...)
	if exSong.Get("map") != "" {
		notes = append(notes, "map: kept from the song: check it against the new sections")
	}
	out := srcDoc{head: head, secs: dr.secs}
	text := out.render()
	if len(dr.secs) > 0 && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text, notes
}

// hasRealSections reports whether d has any section worth keeping: more
// than one, or one with something in it.
func hasRealSections(d srcDoc) bool {
	if len(d.secs) > 1 {
		return true
	}
	for _, s := range d.secs {
		for _, l := range s.lines {
			if l.kind != lkBlank && l.kind != lkComment {
				return true
			}
		}
	}
	return false
}

// mergeHead fills mergeHeaderKeys from drSong into the header zone head
// where exSong has no value: an empty "key:" line is filled in place, a
// missing one goes after the zone's last header line. It returns the new
// zone and the keys added.
func mergeHead(head []string, exSong, drSong *song.Song) ([]string, []string) {
	out := append([]string(nil), head...)
	var added, insert []string
	for _, k := range mergeHeaderKeys {
		v := strings.TrimSpace(drSong.Get(k))
		if v == "" || strings.TrimSpace(exSong.Get(k)) != "" {
			continue
		}
		filled := false
		for i, h := range out {
			if hk, hv, ok := strings.Cut(strings.TrimSpace(h), ":"); ok && strings.TrimSpace(hk) == k && strings.TrimSpace(hv) == "" {
				out[i] = k + ": " + v
				filled = true
				break
			}
		}
		if !filled {
			insert = append(insert, k+": "+v)
		}
		added = append(added, k)
	}
	return insertHeadLines(out, insert), added
}

// insertHeadLines puts lines after the header zone's last header line
// (a known field or a chord diagram), else at its top.
func insertHeadLines(head, lines []string) []string {
	if len(lines) == 0 {
		return head
	}
	last := -1
	for i, h := range head {
		t := strings.TrimSpace(h)
		if strings.HasPrefix(t, "chord:") || strings.HasPrefix(t, "chord[") {
			last = i
			continue
		}
		if k, _, ok := strings.Cut(t, ":"); ok && isHeaderKey(strings.TrimSpace(k)) {
			last = i
		}
	}
	out := append([]string(nil), head[:last+1]...)
	out = append(out, lines...)
	if last < 0 && (len(head) == 0 || strings.TrimSpace(head[0]) != "") {
		out = append(out, "")
	}
	return append(out, head[last+1:]...)
}

// keyNote warns when the song and the draft name different keys: the
// draft's chords are copied as written, never transposed.
func keyNote(exSong, drSong *song.Song) []string {
	a, b := strings.TrimSpace(exSong.Get("key")), strings.TrimSpace(drSong.Get("key"))
	if a == "" || b == "" || chordsym.SameKey(a, b) {
		return nil
	}
	return []string{fmt.Sprintf("Key: the song says %s, the draft %s: its chords were not transposed, check them", a, b)}
}

// displayNames names each section for the notes: its label, numbered
// when the label occurs more than once ("Verse 2").
func displayNames(secs []srcSection) []string {
	total := map[string]int{}
	for _, s := range secs {
		total[strings.ToLower(strings.TrimSpace(s.label))]++
	}
	seen := map[string]int{}
	out := make([]string, len(secs))
	for i, s := range secs {
		label := strings.TrimSpace(s.label)
		l := strings.ToLower(label)
		seen[l]++
		out[i] = label
		if total[l] > 1 && label != "" {
			if _, err := strconv.Atoi(label[len(label)-1:]); err != nil {
				out[i] = fmt.Sprintf("%s %d", label, seen[l])
			}
		}
	}
	return out
}

func orUnnamed(label string) string {
	if label == "" {
		return "(unnamed)"
	}
	return label
}

func secHas(sec *srcSection, k lineKind) bool {
	for _, l := range sec.lines {
		if l.kind == k {
			return true
		}
	}
	return false
}

func hasGP(sec *srcSection) bool {
	for _, l := range sec.lines {
		t := strings.TrimSpace(l.text)
		if l.kind == lkDirective && (strings.HasPrefix(t, "gp:") || strings.HasPrefix(t, "gp[")) {
			return true
		}
	}
	return false
}

// insertGrid puts grid lines in a section that has none: before its
// first lyric or score line, else after its last non-blank line.
func insertGrid(sec *srcSection, grid []string) {
	at := -1
	for i, l := range sec.lines {
		if l.kind == lkLyric || l.kind == lkScore {
			at = i
			break
		}
	}
	if at < 0 {
		at = 0
		for i, l := range sec.lines {
			if l.kind != lkBlank {
				at = i + 1
			}
		}
	}
	var add []srcLine
	for _, g := range grid {
		add = append(add, srcLine{text: g, kind: lkGrid})
	}
	lines := append([]srcLine(nil), sec.lines[:at]...)
	lines = append(lines, add...)
	sec.lines = append(lines, sec.lines[at:]...)
}
