// Package songdiff says what changed between two versions of a .song
// file in the words a band uses — "Verse 2: chords in bar 3 (Am → Am7)",
// "Bass in the Chorus: bars 3–4", "tempo 96 → 102" — rather than as text
// lines, for the song's History page. The .song format makes this
// straightforward: both versions are parsed (internal/song), sections are
// matched by label in order, and each matched pair is compared field by
// field; score voices are decoded (internal/tab) and compared bar by bar.
//
// Lines gives the line-by-line text diff behind it, for readers who want
// the detail, with score voices' long tab data shown as a placeholder.
package songdiff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"app/internal/song"
	"app/internal/tab"
)

// Change is one thing an edit did: Where is the part of the song
// ("Header", "Verse 2", "Bass · Verse 2"), What a short description.
type Change struct {
	Where string
	What  string
}

// Describe lists what changed from before to after, in song order.
func Describe(before, after string) []Change {
	if before == after {
		return nil
	}
	a, b := song.Parse(before), song.Parse(after)
	var out []Change
	out = append(out, headerChanges(a, b)...)
	out = append(out, chordDiagramChanges(a, b)...)
	out = append(out, sectionChanges(a, b)...)
	if len(out) == 0 {
		out = append(out, Change{Where: "Text", What: "layout or comments only"})
	}
	return out
}

func headerChanges(a, b *song.Song) []Change {
	var keys []string
	seen := map[string]bool{}
	for _, s := range []*song.Song{b, a} {
		for _, f := range s.Meta {
			if !seen[f.Key] {
				seen[f.Key] = true
				keys = append(keys, f.Key)
			}
		}
	}
	var out []Change
	for _, k := range keys {
		va, vb := a.Get(k), b.Get(k)
		if va == vb {
			continue
		}
		name := k
		switch k {
		case "map":
			out = append(out, Change{Where: "Header", What: "song map changed"})
			continue
		case "gp_source":
			name = "Guitar Pro file"
		}
		switch {
		case va == "":
			out = append(out, Change{Where: "Header", What: fmt.Sprintf("%s set to %s", name, short(vb))})
		case vb == "":
			out = append(out, Change{Where: "Header", What: fmt.Sprintf("%s removed (was %s)", name, short(va))})
		default:
			out = append(out, Change{Where: "Header", What: fmt.Sprintf("%s %s → %s", name, short(va), short(vb))})
		}
	}
	return out
}

func chordDiagramChanges(a, b *song.Song) []Change {
	old := map[string]string{}
	for _, c := range a.Chords {
		old[c.Name] = c.Spec
	}
	var out []Change
	for _, c := range b.Chords {
		spec, ok := old[c.Name]
		switch {
		case !ok:
			out = append(out, Change{Where: "Chord diagrams", What: c.Name + " added"})
		case spec != c.Spec:
			out = append(out, Change{Where: "Chord diagrams", What: c.Name + " changed"})
		}
		delete(old, c.Name)
	}
	var gone []string
	for name := range old {
		gone = append(gone, name)
	}
	sort.Strings(gone)
	for _, name := range gone {
		out = append(out, Change{Where: "Chord diagrams", What: name + " removed"})
	}
	return out
}

// sectionChanges matches sections by label, in order (a longest common
// subsequence, so a section inserted or removed in the middle doesn't
// make every later one look changed), then compares each matched pair.
func sectionChanges(a, b *song.Song) []Change {
	na, nb := sectionNames(a), sectionNames(b)
	pairs := lcs(len(a.Sections), len(b.Sections), func(i, j int) bool {
		return strings.EqualFold(strings.TrimSpace(a.Sections[i].Label), strings.TrimSpace(b.Sections[j].Label))
	})
	var out []Change
	i, j := 0, 0
	for _, p := range append(pairs, [2]int{len(a.Sections), len(b.Sections)}) {
		for ; i < p[0]; i++ {
			out = append(out, Change{Where: na[i], What: "section removed"})
		}
		for ; j < p[1]; j++ {
			out = append(out, Change{Where: nb[j], What: "section added" + sizeOf(&b.Sections[j])})
		}
		if p[0] < len(a.Sections) {
			out = append(out, compareSection(nb[p[1]], &a.Sections[p[0]], &b.Sections[p[1]])...)
			i, j = p[0]+1, p[1]+1
		}
	}
	return out
}

// sectionNames numbers repeated labels the way the chart does: "Verse 1",
// "Verse 2" when a label comes back, the bare label otherwise.
func sectionNames(s *song.Song) []string {
	total := map[string]int{}
	for _, sec := range s.Sections {
		total[strings.ToLower(strings.TrimSpace(sec.Label))]++
	}
	seen := map[string]int{}
	out := make([]string, len(s.Sections))
	for i, sec := range s.Sections {
		key := strings.ToLower(strings.TrimSpace(sec.Label))
		seen[key]++
		name := strings.TrimSpace(sec.Label)
		if name == "" {
			name = "Untitled section"
		}
		if total[key] > 1 {
			name += " " + strconv.Itoa(seen[key])
		}
		out[i] = name
	}
	return out
}

func sizeOf(sec *song.Section) string {
	switch {
	case sec.Repeat:
		return " (plays an earlier one again)"
	case len(sec.Measures) > 0:
		return fmt.Sprintf(" (%d bars)", len(sec.Measures))
	}
	return ""
}

func compareSection(where string, a, b *song.Section) []Change {
	var out []Change
	add := func(format string, args ...any) { out = append(out, Change{Where: where, What: fmt.Sprintf(format, args...)}) }
	if a.Repeat != b.Repeat {
		if b.Repeat {
			add("now plays the earlier %s again", strings.TrimSpace(b.Label))
		} else {
			add("now has bars of its own")
		}
	}
	if a.Time != b.Time {
		add("time signature %s → %s", orNone(a.Time), orNone(b.Time))
	}
	if a.Times != b.Times {
		add("played %s → %s", timesText(a.Times), timesText(b.Times))
	}
	out = append(out, barChanges(where, a.Measures, b.Measures)...)
	if c := lineChanges(lyricLines(a), lyricLines(b)); c != "" {
		add("lyrics: %s", c)
	}
	if strings.Join(a.Notes, "\n") != strings.Join(b.Notes, "\n") {
		add("notes edited")
	}
	out = append(out, voiceChanges(where, a.Scores, b.Scores)...)
	return out
}

func barChanges(where string, a, b []song.Measure) []Change {
	var changed []int
	var examples []string
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		ta, tb := measureText(a[i]), measureText(b[i])
		if ta != tb {
			changed = append(changed, i)
			examples = append(examples, fmt.Sprintf("bar %d: %s → %s", i+1, ta, tb))
		}
	}
	var out []Change
	switch {
	case len(changed) > 0 && len(changed) <= 2:
		out = append(out, Change{Where: where, What: "chords, " + strings.Join(examples, "; ")})
	case len(changed) > 2:
		out = append(out, Change{Where: where, What: "chords changed in " + barRanges(changed)})
	}
	switch {
	case len(b) > len(a):
		out = append(out, Change{Where: where, What: fmt.Sprintf("%s added (now %d bars)", plural(len(b)-len(a), "bar"), len(b))})
	case len(b) < len(a):
		out = append(out, Change{Where: where, What: fmt.Sprintf("%s removed (now %d bars)", plural(len(a)-len(b), "bar"), len(b))})
	}
	return out
}

// measureText is a bar as the grid writes it: "Am . G7 .", with its
// repeat and ending marks.
func measureText(m song.Measure) string {
	t := strings.Join(m.Tokens, " ")
	if t == "" {
		t = "empty"
	}
	if m.Volta != "" {
		t = "(" + m.Volta + ") " + t
	}
	if m.Open {
		t = "|: " + t
	}
	if m.Close {
		t += " :|"
	}
	return t
}

func lyricLines(sec *song.Section) []string {
	out := make([]string, 0, len(sec.Lyrics))
	for _, l := range sec.Lyrics {
		var b strings.Builder
		for _, s := range l.Segments {
			if s.HasChord {
				b.WriteString("[" + s.Chord + "]")
			}
			b.WriteString(s.Text)
		}
		out = append(out, b.String())
	}
	return out
}

// lineChanges counts lines changed, added and removed between two lists
// ("2 lines changed, 1 added"); "" when they're the same.
func lineChanges(a, b []string) string {
	pairs := lcs(len(a), len(b), func(i, j int) bool { return a[i] == b[j] })
	removed, added := len(a)-len(pairs), len(b)-len(pairs)
	changed := min(removed, added)
	removed -= changed
	added -= changed
	var parts []string
	if changed > 0 {
		parts = append(parts, plural(changed, "line")+" changed")
	}
	if added > 0 {
		parts = append(parts, plural(added, "line")+" added")
	}
	if removed > 0 {
		parts = append(parts, plural(removed, "line")+" removed")
	}
	return strings.Join(parts, ", ")
}

// voiceChanges compares score tracks by label: added, removed, or which
// bars of a track changed.
func voiceChanges(where string, a, b []song.ScoreVoice) []Change {
	var out []Change
	old := map[string]song.ScoreVoice{}
	for _, v := range a {
		old[strings.ToLower(v.Label)] = v
	}
	for _, v := range b {
		key := strings.ToLower(v.Label)
		w := v.Label + " · " + where
		prev, ok := old[key]
		delete(old, key)
		rb, errB := tab.ParseVoice(v.Label, v.Body)
		if !ok {
			what := "track added"
			if errB == nil {
				what += " (" + plural(len(rb.Bars), "bar") + ")"
			}
			out = append(out, Change{Where: w, What: what})
			continue
		}
		if prev.Body == v.Body {
			continue
		}
		ra, errA := tab.ParseVoice(prev.Label, prev.Body)
		if errA != nil || errB != nil {
			out = append(out, Change{Where: w, What: "notation edited"})
			continue
		}
		out = append(out, riffChanges(w, ra, rb)...)
	}
	var gone []string
	for _, v := range a {
		if _, ok := old[strings.ToLower(v.Label)]; ok {
			gone = append(gone, v.Label)
		}
	}
	for _, label := range gone {
		out = append(out, Change{Where: label + " · " + where, What: "track removed"})
	}
	return out
}

func riffChanges(where string, a, b *tab.Riff) []Change {
	var out []Change
	add := func(what string) { out = append(out, Change{Where: where, What: what}) }
	var changed []int
	for i := 0; i < min(len(a.Bars), len(b.Bars)); i++ {
		if !sameBar(a.Bars[i], b.Bars[i]) {
			changed = append(changed, i)
		}
	}
	if len(changed) > 0 {
		add("notes changed in " + barRanges(changed))
	}
	switch {
	case len(b.Bars) > len(a.Bars):
		add(fmt.Sprintf("%s added (now %d)", plural(len(b.Bars)-len(a.Bars), "bar"), len(b.Bars)))
	case len(b.Bars) < len(a.Bars):
		add(fmt.Sprintf("%s removed (now %d)", plural(len(a.Bars)-len(b.Bars), "bar"), len(b.Bars)))
	}
	if a.Pattern != b.Pattern {
		if b.Pattern {
			add("now repeats through its section")
		} else {
			add("no longer repeats through its section")
		}
	}
	if fmt.Sprint(a.Tuning) != fmt.Sprint(b.Tuning) {
		add("tuning changed")
	}
	if len(out) == 0 {
		add("notation edited")
	}
	return out
}

func sameBar(a, b tab.Bar) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// barRanges writes 0-based bar indexes as 1-based ranges: "bars 3–4, 7".
func barRanges(idx []int) string {
	var parts []string
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && idx[j+1] == idx[j]+1 {
			j++
		}
		if j > i {
			parts = append(parts, fmt.Sprintf("%d–%d", idx[i]+1, idx[j]+1))
		} else {
			parts = append(parts, strconv.Itoa(idx[i]+1))
		}
		i = j + 1
	}
	if len(idx) == 1 {
		return "bar " + parts[0]
	}
	return "bars " + strings.Join(parts, ", ")
}

// lcs is the longest common subsequence of two sequences under eq, as
// index pairs in order.
func lcs(n, m int, eq func(i, j int) bool) [][2]int {
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if eq(i, j) {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var out [][2]int
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case eq(i, j):
			out = append(out, [2]int{i, j})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}
	return out
}

func short(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 60 {
		return string(r[:57]) + "…"
	}
	return s
}

func orNone(s string) string {
	if s == "" {
		return "the song's"
	}
	return s
}

func timesText(n int) string {
	if n <= 1 {
		return "once"
	}
	return strconv.Itoa(n) + " times"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// Line is one line of a text diff: Op is '+' (added), '-' (removed), ' '
// (unchanged, kept as context) or '…' (unchanged lines left out).
type Line struct {
	Op   string
	Text string
}

// Lines is the text diff between two versions, with two lines of context
// around each change. A score voice's tab data (one long JSON line) shows
// as "score[Bass]: (tab data)": Describe says what changed in it.
func Lines(before, after string) []Line {
	a, b := strings.Split(before, "\n"), strings.Split(after, "\n")
	pairs := lcs(len(a), len(b), func(i, j int) bool { return a[i] == b[j] })
	var all []Line
	i, j := 0, 0
	for _, p := range append(pairs, [2]int{len(a), len(b)}) {
		for ; i < p[0]; i++ {
			all = append(all, Line{Op: "-", Text: displayLine(a[i])})
		}
		for ; j < p[1]; j++ {
			all = append(all, Line{Op: "+", Text: displayLine(b[j])})
		}
		if p[0] < len(a) {
			all = append(all, Line{Op: " ", Text: displayLine(a[p[0]])})
			i, j = p[0]+1, p[1]+1
		}
	}
	const context = 2
	keep := make([]bool, len(all))
	for k, l := range all {
		if l.Op == " " {
			continue
		}
		for c := max(0, k-context); c <= min(len(all)-1, k+context); c++ {
			keep[c] = true
		}
	}
	var out []Line
	for k, l := range all {
		if keep[k] {
			out = append(out, l)
		} else if len(out) == 0 || out[len(out)-1].Op != "…" {
			out = append(out, Line{Op: "…"})
		}
	}
	return out
}

func displayLine(s string) string {
	if strings.HasPrefix(s, "score[") {
		if k := strings.Index(s, "]:"); k > 0 && strings.HasPrefix(strings.TrimSpace(s[k+2:]), "{\"") {
			return s[:k+2] + " (tab data)"
		}
	}
	return s
}
