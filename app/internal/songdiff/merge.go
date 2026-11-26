package songdiff

import (
	"regexp"
	"strconv"
	"strings"

	"app/internal/song"
)

// The original's changes offered to its versions (docs/ownership-model.md
// §5): a version records the original's chart as it was when the version
// was made or last synced (its base); when the original changes, its
// version's owner sees what changed since (Describe(base, original)) and
// takes the change, part by part where it applies cleanly, or keeps theirs.
//
// A three-way merge by the parts of a song a band talks about: each header
// field (tempo, key, capo, a chord diagram), each section (matched by its
// label and its rank among the sections of that label: "Verse 2"), and the
// order of the sections. A part the original changed and the version
// didn't takes the original's; a part both changed, differently, is the
// version's (a conflict, never mixed line by line).

// metadataKeys are the header fields that say what the song is rather than
// how it goes: a change to them alone is not offered to versions, and
// taking a change never touches them (a version keeps its own title).
var metadataKeys = map[string]bool{
	"title": true, "artist": true, "reference": true, "cover": true, "cover-credit": true,
}

// ChartChanged reports whether after is another chart than before: not
// for whitespace, comments, or the song's metadata (title, artist,
// reference, cover) alone.
func ChartChanged(before, after string) bool {
	if before == after {
		return false
	}
	return chartText(before) != chartText(after)
}

// chartText is a song's chart in canonical form, its metadata left out.
func chartText(body string) string {
	s := song.Parse(body)
	meta := s.Meta[:0]
	for _, f := range s.Meta {
		if !metadataKeys[f.Key] {
			meta = append(meta, f)
		}
	}
	s.Meta = meta
	var out []string
	blank := false
	for _, l := range strings.Split(song.Write(s), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			blank = true
			continue
		}
		if blank && len(out) > 0 {
			out = append(out, "")
		}
		blank = false
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// Part is one part of a song the original changed, as the version's
// owner chooses it: Key names it for Merge; Kind is "field" (a header
// field, Name "tempo"), "chord" (a chord diagram, Name the chord),
// "section" (Name "Verse 2") or "order" (the order of the sections); What
// is "changed", "added", "removed" or "moved"; Conflict: the version
// changed it too, otherwise — taking the change would undo theirs, so it
// stays theirs.
type Part struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	What     string `json:"what"`
	Conflict bool   `json:"conflict"`
}

// Plan lists the parts the original (theirs) changed since base that the
// version (ours) doesn't have yet, in song order: the header's fields,
// then the sections, then their order.
func Plan(base, ours, theirs string) []Part {
	b, o, t := splitSong(base), splitSong(ours), splitSong(theirs)
	var out []Part
	for _, k := range unionKeys(t.headKeys, b.headKeys) {
		bv, bok := b.head[k]
		ov, ook := o.head[k]
		tv, tok := t.head[k]
		if bv == tv && bok == tok {
			continue
		}
		if ov == tv && ook == tok {
			continue
		}
		what := "changed"
		switch {
		case !bok:
			what = "added"
		case !tok:
			what = "removed"
		}
		kind, name := headName(k)
		out = append(out, Part{Key: "h:" + k, Kind: kind, Name: name, What: what, Conflict: ov != bv || ook != bok})
	}
	for _, k := range unionKeys(t.secKeys, b.secKeys) {
		bv, bok := b.secs[k]
		ov, ook := o.secs[k]
		tv, tok := t.secs[k]
		if bok == tok && bv.text == tv.text {
			continue
		}
		if ook == tok && ov.text == tv.text {
			continue
		}
		name := tv.name
		what := "changed"
		switch {
		case !bok:
			what = "added"
		case !tok:
			what, name = "removed", bv.name
		}
		out = append(out, Part{Key: "s:" + k, Kind: "section", Name: name, What: what, Conflict: ook != bok || ov.text != bv.text})
	}
	bo, oo, to := commonOrder(b, o, t), commonOrder(o, b, t), commonOrder(t, b, o)
	if !sameKeys(to, bo) && !sameKeys(oo, to) {
		out = append(out, Part{Key: "order", Kind: "order", What: "moved", Conflict: !sameKeys(oo, bo)})
	}
	return out
}

// Merge is ours with the parts take says yes to taken from theirs (base
// is what both came from). A conflicting part is never taken, whatever
// take says.
func Merge(base, ours, theirs string, take func(key string) bool) string {
	b, o, t := splitSong(base), splitSong(ours), splitSong(theirs)
	parts := Plan(base, ours, theirs)
	taken := map[string]bool{}
	for _, p := range parts {
		if !p.Conflict && take(p.Key) {
			taken[p.Key] = true
		}
	}
	if len(taken) == 0 {
		return ours
	}

	// The header: ours, its fields replaced, removed or added.
	var head []string
	done := map[string]bool{}
	lastField := -1
	for _, l := range o.headLines {
		k := headKey(l)
		if k != "" && taken["h:"+k] {
			done[k] = true
			if tv, ok := t.head[k]; ok {
				head = append(head, tv)
				lastField = len(head) - 1
			}
			continue
		}
		head = append(head, l)
		if k != "" {
			lastField = len(head) - 1
		}
	}
	var added []string
	for _, k := range t.headKeys {
		if taken["h:"+k] && !done[k] {
			added = append(added, t.head[k])
		}
	}
	if len(added) > 0 {
		at := lastField + 1
		head = append(head[:at], append(added, head[at:]...)...)
	}

	// The sections: ours, in ours' order or theirs' when the order is
	// taken; a section taken from theirs in its place there.
	order := append([]string{}, o.secKeys...)
	if taken["order"] {
		order = reorderLike(order, t.secKeys)
	}
	texts := map[string]string{}
	for _, k := range o.secKeys {
		texts[k] = o.secs[k].text
	}
	for _, k := range unionKeys(t.secKeys, b.secKeys) {
		if !taken["s:"+k] {
			continue
		}
		tv, tok := t.secs[k]
		if !tok {
			delete(texts, k)
			order = without(order, k)
			continue
		}
		if _, in := texts[k]; !in {
			order = insertAfter(order, k, t.secKeys)
		}
		texts[k] = tv.text
	}

	blocks := []string{trimBlank(strings.Join(head, "\n"))}
	for _, k := range order {
		blocks = append(blocks, trimBlank(texts[k]))
	}
	var out []string
	for _, bl := range blocks {
		if bl != "" {
			out = append(out, bl)
		}
	}
	return strings.Join(out, "\n\n") + "\n"
}

// sectionHeaderRE is internal/song's: a line that is one "[Label]" alone.
var sectionHeaderRE = regexp.MustCompile(`^\[([^\[\]]+)\](\s*\(repeat\))?$`)

type section struct {
	name string // "Verse 2"
	text string // its lines, its "[Verse]" line first
}

type splitBody struct {
	headLines []string
	headKeys  []string
	head      map[string]string // field → its line
	secKeys   []string
	secs      map[string]section
}

// splitSong cuts a song's text into its header's fields and its sections,
// each keyed: a field by its name ("tempo", "chord[G] Am"), a section by
// its label and rank ("verse#2").
func splitSong(body string) splitBody {
	sb := splitBody{head: map[string]string{}, secs: map[string]section{}}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	i := 0
	for ; i < len(lines); i++ {
		if sectionHeaderRE.MatchString(strings.TrimSpace(lines[i])) {
			break
		}
		sb.headLines = append(sb.headLines, lines[i])
		if k := headKey(lines[i]); k != "" {
			if _, dup := sb.head[k]; !dup {
				sb.headKeys = append(sb.headKeys, k)
			}
			sb.head[k] = strings.TrimSpace(lines[i])
		}
	}
	total, seen := map[string]int{}, map[string]int{}
	type raw struct {
		label string
		lines []string
	}
	var raws []raw
	for ; i < len(lines); i++ {
		if m := sectionHeaderRE.FindStringSubmatch(strings.TrimSpace(lines[i])); m != nil {
			raws = append(raws, raw{label: strings.TrimSpace(m[1])})
			total[strings.ToLower(strings.TrimSpace(m[1]))]++
		}
		raws[len(raws)-1].lines = append(raws[len(raws)-1].lines, lines[i])
	}
	for _, r := range raws {
		low := strings.ToLower(r.label)
		seen[low]++
		key := low + "#" + itoa(seen[low])
		name := r.label
		if total[low] > 1 {
			name += " " + itoa(seen[low])
		}
		sb.secKeys = append(sb.secKeys, key)
		sb.secs[key] = section{name: name, text: trimBlank(strings.Join(r.lines, "\n"))}
	}
	return sb
}

// headKey is a header line's field ("tempo"; a chord diagram's key and
// name, "chord[G] Am"), "" for a blank line, a comment or the song's
// metadata (which a merge leaves as it is).
func headKey(line string) string {
	l := strings.TrimSpace(line)
	if l == "" || strings.HasPrefix(l, "#") {
		return ""
	}
	i := strings.Index(l, ":")
	if i <= 0 {
		return ""
	}
	k := strings.TrimSpace(l[:i])
	if strings.HasPrefix(k, "chord") {
		f := strings.Fields(l[i+1:])
		if len(f) == 0 {
			return ""
		}
		return k + " " + f[0]
	}
	if metadataKeys[k] {
		return ""
	}
	return k
}

// headName is what a header field is ("field", "chord") and how it is
// named to people: "tempo", a chord diagram's chord.
func headName(k string) (string, string) {
	if strings.HasPrefix(k, "chord") {
		if i := strings.Index(k, " "); i > 0 {
			return "chord", k[i+1:]
		}
	}
	if k == "gp_source" {
		return "field", "Guitar Pro file"
	}
	return "field", k
}

// unionKeys is a's keys, then b's that a lacks, each once.
func unionKeys(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range [][]string{a, b} {
		for _, k := range l {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	return out
}

// commonOrder is x's section keys that y and z have too, in x's order.
func commonOrder(x, y, z splitBody) []string {
	var out []string
	for _, k := range x.secKeys {
		if _, ok := y.secs[k]; !ok {
			continue
		}
		if _, ok := z.secs[k]; !ok {
			continue
		}
		out = append(out, k)
	}
	return out
}

func sameKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// reorderLike puts order's keys that like has in like's order, each other
// key staying right after the one it followed.
func reorderLike(order, like []string) []string {
	in := map[string]bool{}
	for _, k := range like {
		in[k] = true
	}
	pos := map[string]bool{}
	for _, k := range order {
		pos[k] = true
	}
	var out []string
	for _, k := range like {
		if pos[k] {
			out = append(out, k)
		}
	}
	for i, k := range order {
		if in[k] {
			continue
		}
		// After the nearest earlier key of order's that is placed.
		at := 0
		for j := i - 1; j >= 0; j-- {
			if idx := indexOf(out, order[j]); idx >= 0 {
				at = idx + 1
				break
			}
		}
		out = append(out[:at], append([]string{k}, out[at:]...)...)
	}
	return out
}

// insertAfter puts k into order after the nearest key before it in like
// that order has (at the start when none).
func insertAfter(order []string, k string, like []string) []string {
	i := indexOf(like, k)
	at := 0
	for j := i - 1; j >= 0; j-- {
		if idx := indexOf(order, like[j]); idx >= 0 {
			at = idx + 1
			break
		}
	}
	return append(order[:at:at], append([]string{k}, order[at:]...)...)
}

func without(order []string, k string) []string {
	out := order[:0:0]
	for _, x := range order {
		if x != k {
			out = append(out, x)
		}
	}
	return out
}

func indexOf(l []string, k string) int {
	for i, x := range l {
		if x == k {
			return i
		}
	}
	return -1
}

// trimBlank drops a block's trailing blank lines (and its leading ones).
func trimBlank(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

func itoa(n int) string { return strconv.Itoa(n) }

// ChangedSections lists the sections of after (their indexes, in order)
// that before doesn't have as they are: new or changed, a section matched
// by its label and its rank among the sections of that label.
func ChangedSections(before, after string) []int {
	b, a := splitSong(before), splitSong(after)
	var out []int
	for i, k := range a.secKeys {
		if bv, ok := b.secs[k]; !ok || bv.text != a.secs[k].text {
			out = append(out, i)
		}
	}
	return out
}

// MergeAll is ours with every part theirs changed since base that doesn't
// conflict, the song's metadata included (title, artist, reference,
// cover: Merge leaves them, a version keeping its own; a merge of two
// edits of one song takes theirs where only theirs changed it).
func MergeAll(base, ours, theirs string) string {
	out := Merge(base, ours, theirs, func(string) bool { return true })
	for k := range metadataKeys {
		bv, tv, ov := metaLine(base, k), metaLine(theirs, k), metaLine(ours, k)
		if bv != tv && ov == bv {
			out = setMetaLine(out, k, tv)
		}
	}
	return out
}

// metaLine is a header field's line ("" when absent).
func metaLine(body, key string) string {
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		if sectionHeaderRE.MatchString(t) {
			break
		}
		if k, _, ok := strings.Cut(t, ":"); ok && strings.TrimSpace(k) == key {
			return t
		}
	}
	return ""
}

// setMetaLine puts line as body's header field key (drops it for "").
func setMetaLine(body, key, line string) string {
	lines := strings.Split(body, "\n")
	end := len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if sectionHeaderRE.MatchString(t) {
			end = i
			break
		}
		if k, _, ok := strings.Cut(t, ":"); ok && strings.TrimSpace(k) == key {
			if line == "" {
				return strings.Join(append(lines[:i:i], lines[i+1:]...), "\n")
			}
			lines[i] = line
			return strings.Join(lines, "\n")
		}
	}
	if line == "" {
		return body
	}
	// After the header's last field.
	at := 0
	for i := 0; i < end; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			at = i + 1
		}
	}
	lines = append(lines[:at], append([]string{line}, lines[at:]...)...)
	return strings.Join(lines, "\n")
}
