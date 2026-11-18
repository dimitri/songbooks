package importer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"app/internal/song"
)

// CleanOptions tunes CleanSongText.
type CleanOptions struct {
	// StripInlineChords drops "[Chord]" markers from lyric lines. Grid
	// lines keep their chords.
	StripInlineChords bool
}

// CleanSongText tidies an imported .song draft and says what it changed
// (short human notes, no lyric text in them).
//
// It works line by line inside each section, the same way
// internal/song.Parse reads the file, and never reorders sections or
// touches header lines (other than adding "capo:"), comments, directives
// (time:, bars:, times:, note:, gp:) or score[...] blocks. What it does:
//
//   - "Capo N" lines anywhere move to a "capo: N" header when none is set.
//   - Noise lines go: credits ("Tabbed by"), "Chords used:", tuning, key,
//     tempo, rating/footer lines, URLs, rows of "****", chord fingering
//     legends, "repeat chorus" directions.
//   - Tab staff lines ("e|---3---|") go.
//   - A chord-only line in a lyric block goes when the section has a grid,
//     and becomes the section's grid when it has none.
//   - A "chords used" dictionary swallowed into the end of a grid is cut
//     (see cutChordDictionaries).
//   - Repeat marks ("x2", "(x2)", "[x4]", "(bis)") come off lyric lines. A
//     standalone mark ending a section with a grid becomes "times: N" (the
//     parser's own section repeat count); other marks are dropped.
//   - Lyric lines that only repeat a section label ("Chorus:", "Intro:")
//     go.
//   - Blank-line runs collapse to one, trailing spaces go.
//   - Sections the cleaning left empty go (all their lines were junk),
//     except "(repeat)" sections, Solo sections and references to an
//     earlier section with the same label. A section that was already
//     bare stays: bare sections are this app's structure markers.
//
// The result is a fixed point: CleanSongText(CleanSongText(x)) ==
// CleanSongText(x).
func CleanSongText(text string, opts CleanOptions) (string, []string) {
	var notes []string
	cur := text
	for i := 0; i < 5; i++ {
		next, n := cleanOnce(cur, opts)
		notes = append(notes, n...)
		if next == cur {
			break
		}
		cur = next
	}
	return cur, notes
}

// lineKind is how internal/song.Parse would read a line inside a section.
type lineKind int

const (
	lkBlank lineKind = iota
	lkComment
	lkDirective // time:, bars:, times:, note:, gp..., score: (dropped by the parser)
	lkScore     // a score[...] block, header and continuation lines
	lkGrid      // grid: lines, and "|" lines before any lyric line
	lkLyric
	lkRepeat // CleanSongText only: a standalone repeat mark awaiting a decision
)

type srcLine struct {
	text string
	kind lineKind
	n    int // lkRepeat: the repeat count
}

type srcSection struct {
	header string // the header line as written
	label  string
	repeat bool
	lines  []srcLine
}

// srcDoc is a .song text split the way the parser splits it: the header
// zone (every line before the first section header, kept verbatim) and
// the sections, each line classified.
type srcDoc struct {
	head []string
	secs []srcSection
}

// songSectionHeaderRE is internal/song's own section header rule.
var songSectionHeaderRE = regexp.MustCompile(`^\[([^\[\]]+)\](\s*\(repeat\))?$`)

// scanDoc classifies text line by line, mirroring internal/song.Parse
// (comments first, then section headers, then an open score block, then
// the directive switch). Joining every line back with "\n" gives text
// back unchanged (see render).
func scanDoc(text string) srcDoc {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var d srcDoc
	cur := -1
	inScore, depth, hasLyric := false, 0, false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		isComment := strings.HasPrefix(line, "#")
		if !isComment {
			if m := songSectionHeaderRE.FindStringSubmatch(line); m != nil {
				d.secs = append(d.secs, srcSection{header: raw, label: m[1], repeat: m[2] != ""})
				cur = len(d.secs) - 1
				inScore, depth, hasLyric = false, 0, false
				continue
			}
		}
		if cur < 0 {
			d.head = append(d.head, raw)
			continue
		}
		add := func(k lineKind) { d.secs[cur].lines = append(d.secs[cur].lines, srcLine{text: raw, kind: k}) }
		if isComment {
			add(lkComment)
			continue
		}
		if inScore {
			if depth > 0 || scoreLineContinues(line) {
				add(lkScore)
				depth += strings.Count(line, "{") - strings.Count(line, "}")
				if depth < 0 {
					depth = 0
				}
				continue
			}
			inScore = false
		}
		switch {
		case line == "":
			add(lkBlank)
		case strings.HasPrefix(line, "time:"):
			add(lkDirective)
		case strings.HasPrefix(line, "score["):
			rest := line[len("score["):]
			if end := strings.IndexByte(rest, ']'); end >= 0 {
				val := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest[end+1:]), ":"))
				inScore = true
				depth = strings.Count(val, "{") - strings.Count(val, "}")
				if depth < 0 {
					depth = 0
				}
				add(lkScore)
			} else {
				add(lkDirective)
			}
		case strings.HasPrefix(line, "gp[") || strings.HasPrefix(line, "gp:"):
			add(lkDirective)
		case strings.HasPrefix(line, "grid:"):
			add(lkGrid)
		case strings.HasPrefix(line, "bars:"), strings.HasPrefix(line, "times:"),
			strings.HasPrefix(line, "note:"), strings.HasPrefix(line, "score:"):
			add(lkDirective)
		case strings.HasPrefix(line, "|") && !hasLyric:
			add(lkGrid)
		default:
			add(lkLyric)
			hasLyric = true
		}
	}
	return d
}

// scoreLineContinues mirrors internal/song's scoreContinues.
func scoreLineContinues(line string) bool {
	if line == "" || songSectionHeaderRE.MatchString(line) {
		return false
	}
	for _, p := range [...]string{"time:", "grid:", "bars:", "times:", "note:", "score:", "score[", "gp:", "gp["} {
		if strings.HasPrefix(line, p) {
			return false
		}
	}
	return true
}

// render joins a srcDoc back into text, every line as stored.
func (d srcDoc) render() string {
	out := append([]string(nil), d.head...)
	for _, s := range d.secs {
		out = append(out, s.header)
		for _, l := range s.lines {
			out = append(out, l.text)
		}
	}
	return strings.Join(out, "\n")
}

// ---- line classifiers shared with lyricsplace.go ----

var (
	clCapoRE      = regexp.MustCompile(`(?i)^capo\s*(?:on|at)?\s*:?\s*(?:on|at)?\s*(?:the\s+)?(\d{1,2})(?:st|nd|rd|th)?(?:\s+fret)?\.?$`)
	clNoCapoRE    = regexp.MustCompile(`(?i)^(?:no capo|capo\s*:?\s*(?:none|no|0|-))\.?$`)
	clNoiseByRE   = regexp.MustCompile(`(?i)^(?:tabbed|transcribed|arranged|submitted|corrected|edited|chords|tabs?)\s+(?:by|from)\b`)
	clNoiseKeyRE  = regexp.MustCompile(`(?i)^(?:chords used|chords|chord list|tuning|key|tempo|bpm|difficulty|strumming(?: pattern)?|album|artist|song|title|author|composer|written by|lyrics by|music by|source|e-?mail|last updated?|rating)\s*:`)
	clTuningRE    = regexp.MustCompile(`(?i)^(?:(?:standard|std\.?|drop\s+[a-g][#b]?|half[- ]step down|e\s*a\s*d\s*g\s*b\s*e)\s+)?tuning\.?(?:\s*\(?[a-g#b ]+\)?)?$`)
	clSymbolsRE   = regexp.MustCompile(`^[*\-=_~.•·+]{3,}$`)
	clURLRE       = regexp.MustCompile(`(?i)https?://|www\.`)
	clFooterRE    = regexp.MustCompile(`(?i)^(?:chords used.*|please rate.*|rate this tab.*|enjoy\s*!*|you might also like|\d*\s*embed|\d+\s+contributors?.*)$`)
	clRepeatDirRE = regexp.MustCompile(`(?i)^[(\[]?\s*(?:repeat|rpt\.?)(?:\s+(?:the|chorus|verse|refrain|bridge|intro|outro|riff|last|line|all|twice|\d+\s*times|[x×]\s*\d+|\d+\s*[x×]))*\s*[)\]]?\s*:?$`)
	// clLegendRE is a chord fingering legend line ("Em7  0-2-2-0-3-3").
	clLegendRE = regexp.MustCompile(`^(\S+)\s+[0-9xX]+(?:-[0-9xX]+)+\s*$`)
	// clTabStaffRE is one line of ASCII tablature: a string name, then
	// "|" or "-", then only fret/technique characters.
	clTabStaffRE = regexp.MustCompile(`^(?:[a-gA-G][#b]?\s*)?[|:\-][-0-9hpbrxXsS/\\~()\[\]<>.^*|=: ]*$`)

	// clRepeatOnlyRE is a line that is only a repeat mark.
	clRepeatOnlyRE = regexp.MustCompile(`(?i)^[(\[]?\s*(?:[x×]\s*(\d+)|(\d+)\s*[x×]|(bis))\s*[)\]]?$`)
	// clRepeatTailRE is a repeat mark ending a lyric line.
	clRepeatTailRE = regexp.MustCompile(`(?i)(?:\s*[(\[]\s*(?:[x×]\s*(\d+)|(\d+)\s*[x×]|(bis))\s*[)\]]|\s+(?:[x×]\s*(\d+)|(\d+)\s*[x×]))\s*$`)
	// clRepeatHeaderRE is a "[x4]" line, which the parser would read as a
	// section header.
	clRepeatHeaderRE = regexp.MustCompile(`(?i)^\[\s*([x×]\s*\d+|\d+\s*[x×])\s*\]$`)

	clInlineChordRE = regexp.MustCompile(`\[[^\]\n]*\]`)
)

// capoValue reads a "Capo 2", "Capo: 2", "capo on 3rd fret" line.
func capoValue(t string) (string, bool) {
	if m := clCapoRE.FindStringSubmatch(t); m != nil {
		n, _ := strconv.Atoi(m[1])
		return strconv.Itoa(n), true
	}
	return "", false
}

// isTabStaff reports a line of ASCII tablature ("e|---3---|", "|--5-7--|").
func isTabStaff(t string) bool {
	return strings.Count(t, "-") >= 3 && clTabStaffRE.MatchString(t)
}

// isNoiseLine reports a line carrying nothing a .song section can use.
func isNoiseLine(t string) bool {
	if clNoCapoRE.MatchString(t) || clNoiseByRE.MatchString(t) || clNoiseKeyRE.MatchString(t) ||
		clTuningRE.MatchString(t) || clURLRE.MatchString(t) || clFooterRE.MatchString(t) ||
		clRepeatDirRE.MatchString(t) {
		return true
	}
	if clSymbolsRE.MatchString(strings.ReplaceAll(t, " ", "")) {
		return true
	}
	if m := clLegendRE.FindStringSubmatch(t); m != nil && looksLikeChord(m[1]) {
		return true
	}
	return false
}

// repeatCount reads the count out of a clRepeatOnlyRE/clRepeatTailRE
// match ("bis" counts 2).
func repeatCount(m []string) int {
	for _, g := range m[1:] {
		if g == "" {
			continue
		}
		if strings.EqualFold(g, "bis") {
			return 2
		}
		if n, err := strconv.Atoi(g); err == nil {
			return n
		}
	}
	return 0
}

// standaloneRepeat reports a line that is only "x2", "(x2)", "2x", "(bis)".
func standaloneRepeat(t string) (int, bool) {
	if m := clRepeatOnlyRE.FindStringSubmatch(t); m != nil {
		return repeatCount(m), true
	}
	return 0, false
}

// stripRepeatTail removes a trailing repeat mark from a lyric line.
func stripRepeatTail(t string) (string, bool) {
	loc := clRepeatTailRE.FindStringIndex(t)
	if loc == nil || loc[0] == 0 {
		return t, false
	}
	return strings.TrimRight(t[:loc[0]], " \t"), true
}

// chordSeparators are tokens a hand-typed chord row uses between chords.
var chordSeparators = map[string]bool{"-": true, "–": true, "—": true, "/": true, "//": true, "->": true, "→": true, "..": true, "...": true, "~": true}

// chordOnlyLine recognizes a row of chords with no lyric text: "[Am] [G]",
// "Am  G  F  C", "Intro: Am G x2", "| C | G |". chords keeps "%" as
// written; repeat is a trailing "x2" count (0 when none).
func chordOnlyLine(t string) (chords []string, repeat int, ok bool) {
	t = strings.TrimSpace(t)
	if i := strings.IndexByte(t, ':'); i > 0 && strictLabel(t[:i]) {
		t = strings.TrimSpace(t[i+1:])
	}
	if m := clRepeatTailRE.FindStringSubmatchIndex(t); m != nil && m[0] > 0 {
		groups := make([]string, len(m)/2)
		for g := range groups {
			if m[2*g] >= 0 {
				groups[g] = t[m[2*g]:m[2*g+1]]
			}
		}
		repeat = repeatCount(groups)
		t = strings.TrimSpace(t[:m[0]])
	}
	bracketed := strings.ContainsAny(t, "[|")
	t = strings.NewReplacer("[", " ", "]", " ", "|", " ", "(", " ", ")", " ", ",", " ").Replace(t)
	real := 0
	for _, f := range strings.Fields(t) {
		f = strings.Trim(f, ":")
		if f == "" || chordSeparators[f] {
			continue
		}
		if f == "%" {
			chords = append(chords, f)
			continue
		}
		if !looksLikeChord(f) {
			return nil, 0, false
		}
		chords = append(chords, f)
		if f != "N.C." {
			real++
		}
	}
	if real == 0 {
		return nil, 0, false
	}
	// A lone bare capital letter ("A") is as likely a lyric as a chord.
	if len(chords) == 1 && !bracketed && len(chords[0]) == 1 {
		return nil, 0, false
	}
	return chords, repeat, true
}

// chordsToGrid turns a chord row into a grid: line the way the UG
// importer builds one (one bar per chord, "%" for a held chord, a repeat
// count as a "|: :|" bracket).
func chordsToGrid(chords []string, repeat int) string {
	var toks []UGToken
	prev := ""
	for _, c := range chords {
		if c == "%" {
			if prev == "" {
				continue
			}
			c = prev
		}
		toks = append(toks, UGToken{Chord: c})
		prev = c
	}
	return "grid: " + UGSection{Lines: []UGLine{{Tokens: toks}}, RepeatCount: repeat}.Grid()
}

// isLabelLine reports a lyric line that only names a section: the
// section's own label, or any known section word ("Chorus:", "(Intro)",
// "Couplet 2").
func isLabelLine(t, ownLabel string) bool {
	inner := unwrapLabel(t)
	if inner == "" {
		return false
	}
	if a, b := strings.Join(labelTokens(inner), " "), strings.Join(labelTokens(ownLabel), " "); a != "" && a == b {
		return true
	}
	return strictLabel(inner)
}

// unwrapLabel strips the wrapping a label line carries: "(", "[", a
// trailing ":" and a trailing repeat mark ("Chorus x2").
func unwrapLabel(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimSpace(strings.TrimSuffix(t, ":"))
	if len(t) >= 2 && ((t[0] == '(' && t[len(t)-1] == ')') || (t[0] == '[' && t[len(t)-1] == ']')) {
		t = strings.TrimSpace(t[1 : len(t)-1])
	}
	t = strings.TrimSpace(strings.TrimSuffix(t, ":"))
	if s, ok := stripRepeatTail(t); ok {
		t = s
	}
	return t
}

// ---- the cleaning pass ----

// cleanOnce is one pass of CleanSongText.
func cleanOnce(text string, opts CleanOptions) (string, []string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = unbracketRepeatHeaders(text)
	doc := scanDoc(text)
	var notes []string

	// Header zone: only "Capo N" lines are touched.
	capo, metaIdx, metaVal := "", -1, ""
	var head []string
	for _, h := range doc.head {
		t := strings.TrimSpace(h)
		if strings.HasPrefix(t, "capo:") {
			if metaIdx < 0 {
				metaIdx, metaVal = len(head), strings.TrimSpace(t[len("capo:"):])
			}
			head = append(head, h)
			continue
		}
		if v, ok := capoValue(t); ok {
			if capo == "" {
				capo = v
			}
			continue
		}
		head = append(head, h)
	}

	hadLines := make([]bool, len(doc.secs))
	for i := range doc.secs {
		for _, l := range doc.secs[i].lines {
			if l.kind != lkBlank {
				hadLines[i] = true
			}
		}
		notes = append(notes, cleanSection(&doc.secs[i], opts, &capo)...)
	}
	notes = append(notes, cutChordDictionaries(&doc)...)
	notes = append(notes, dropEmptySections(&doc, hadLines)...)

	if capo != "" {
		switch {
		case metaIdx >= 0 && metaVal == "":
			head[metaIdx] = "capo: " + capo
			notes = append(notes, "filled in capo: "+capo+" in the header")
		case metaIdx >= 0:
			notes = append(notes, "dropped a Capo line (the header already sets capo)")
		default:
			// After the last real header field, else after the last
			// non-blank line of the header zone.
			last, lastMeta := -1, -1
			for i, h := range head {
				t := strings.TrimSpace(h)
				if t == "" {
					continue
				}
				last = i
				if k, _, ok := strings.Cut(t, ":"); ok && isHeaderKey(strings.TrimSpace(k)) {
					lastMeta = i
				}
			}
			if lastMeta >= 0 {
				last = lastMeta
			}
			head = append(head[:last+1], append([]string{"capo: " + capo}, head[last+1:]...)...)
			notes = append(notes, "moved Capo "+capo+" to a capo: header")
		}
	}
	doc.head = head
	return renderClean(doc), notes
}

func isHeaderKey(k string) bool {
	for _, mk := range song.MetaKeys {
		if mk == k {
			return true
		}
	}
	return false
}

// unbracketRepeatHeaders turns a "[x4]" line after the first real section
// header into a plain "x4" line, so it reads as the repeat mark it is
// rather than a section of its own.
func unbracketRepeatHeaders(text string) string {
	lines := strings.Split(text, "\n")
	seen := false
	for i, raw := range lines {
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "#") {
			continue
		}
		if seen {
			if m := clRepeatHeaderRE.FindStringSubmatch(t); m != nil {
				lines[i] = m[1]
				continue
			}
		}
		if songSectionHeaderRE.MatchString(t) {
			seen = true
		}
	}
	return strings.Join(lines, "\n")
}

// renderClean writes a cleaned doc: the header zone (blank lines around it
// trimmed), one blank line, then each section followed by one blank line.
func renderClean(d srcDoc) string {
	head := d.head
	for len(head) > 0 && strings.TrimSpace(head[0]) == "" {
		head = head[1:]
	}
	for len(head) > 0 && strings.TrimSpace(head[len(head)-1]) == "" {
		head = head[:len(head)-1]
	}
	var out []string
	if len(head) > 0 {
		out = append(out, head...)
		out = append(out, "")
	}
	for _, s := range d.secs {
		out = append(out, strings.TrimSpace(s.header))
		for _, l := range s.lines {
			out = append(out, l.text)
		}
		out = append(out, "")
	}
	res := strings.TrimRight(strings.Join(out, "\n"), "\n")
	if res == "" {
		return ""
	}
	return res + "\n"
}

// cleanSection applies the per-line rules to one section and returns its
// notes. capo receives the first "Capo N" value found.
func cleanSection(sec *srcSection, opts CleanOptions, capo *string) []string {
	label := strings.TrimSpace(sec.label)
	hasGrid := false
	for _, l := range sec.lines {
		if l.kind == lkGrid {
			hasGrid = true
		}
	}
	var c struct{ tab, capo, noise, chordDrop, labels, tailRepeats, inline, standalone int }
	var gridAdds []srcLine
	var out []srcLine
	for _, l := range sec.lines {
		if l.kind == lkScore {
			out = append(out, l)
			continue
		}
		l.text = strings.TrimRight(l.text, " \t")
		t := strings.TrimSpace(l.text)
		switch l.kind {
		case lkGrid:
			if strings.HasPrefix(t, "|") && isTabStaff(t) {
				c.tab++
				continue
			}
			out = append(out, l)
			continue
		case lkBlank:
			l.text = ""
			out = append(out, l)
			continue
		case lkLyric:
		default:
			out = append(out, l)
			continue
		}
		if isTabStaff(t) {
			c.tab++
			continue
		}
		if v, ok := capoValue(t); ok {
			if *capo == "" {
				*capo = v
			}
			c.capo++
			continue
		}
		if isNoiseLine(t) {
			c.noise++
			continue
		}
		if n, ok := standaloneRepeat(t); ok {
			out = append(out, srcLine{text: t, kind: lkRepeat, n: n})
			continue
		}
		if chords, rep, ok := chordOnlyLine(t); ok {
			if hasGrid {
				c.chordDrop++
			} else {
				gridAdds = append(gridAdds, srcLine{text: chordsToGrid(chords, rep), kind: lkGrid})
			}
			continue
		}
		if isLabelLine(t, label) {
			c.labels++
			continue
		}
		changed := false
		if s, ok := stripRepeatTail(t); ok {
			t, changed = s, true
			c.tailRepeats++
		}
		if opts.StripInlineChords && strings.Contains(t, "[") {
			if s := strings.Join(strings.Fields(clInlineChordRE.ReplaceAllString(t, " ")), " "); s != t {
				t, changed = s, true
				c.inline++
			}
		}
		if t == "" {
			continue
		}
		if changed {
			l.text = t
		}
		out = append(out, l)
	}
	if len(gridAdds) > 0 {
		out = append(gridAdds, out...)
		hasGrid = true
	}

	// A standalone repeat mark ending a section with a grid becomes the
	// parser's own section repeat count ("times: N"), unless the grid
	// already brackets a repeat or a times: line exists; any other one is
	// dropped.
	var notes []string
	hasTimes, gridRepeats := false, false
	lastContent := -1
	for i, l := range out {
		t := strings.TrimSpace(l.text)
		if l.kind == lkDirective && strings.HasPrefix(t, "times:") {
			hasTimes = true
		}
		if l.kind == lkGrid && (strings.Contains(t, "|:") || strings.Contains(t, ":|")) {
			gridRepeats = true
		}
		if l.kind != lkBlank && l.kind != lkComment {
			lastContent = i
		}
	}
	var final []srcLine
	for i, l := range out {
		if l.kind != lkRepeat {
			final = append(final, l)
			continue
		}
		if i == lastContent && hasGrid && !hasTimes && !gridRepeats && l.n >= 2 {
			final = append(final, srcLine{text: "times: " + strconv.Itoa(l.n), kind: lkDirective})
			notes = append(notes, fmt.Sprintf("%s: turned a standalone x%d into times: %d", label, l.n, l.n))
			continue
		}
		c.standalone++
	}

	// Blank-line runs collapse to one; none at either end.
	var res []srcLine
	for _, l := range final {
		if l.kind == lkBlank && (len(res) == 0 || res[len(res)-1].kind == lkBlank) {
			continue
		}
		res = append(res, l)
	}
	for len(res) > 0 && res[len(res)-1].kind == lkBlank {
		res = res[:len(res)-1]
	}
	sec.lines = res

	add := func(n int, format string) {
		if n > 0 {
			notes = append(notes, fmt.Sprintf("%s: "+format, label, n, plural(n)))
		}
	}
	add(c.tab, "dropped %d tab staff line%s")
	add(c.noise, "dropped %d noise line%s (credits, tuning, chord lists, links…)")
	add(c.chordDrop, "dropped %d chord-only line%s (the grid already has the chords)")
	add(len(gridAdds), "turned %d chord-only line%s into the grid")
	add(c.labels, "dropped %d line%s that only named a section")
	add(c.tailRepeats, "removed %d repeat mark%s from lyric lines (add times: N if the whole section repeats)")
	add(c.standalone, "dropped %d standalone repeat mark%s")
	add(c.inline, "removed inline chords from %d lyric line%s")
	if c.capo > 0 {
		notes = append(notes, fmt.Sprintf("%s: took a Capo line out of the lyrics", label))
	}
	return notes
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// gridBar is one measure of a section's grid, located in the source so a
// tail of bars can be cut off.
type gridBar struct {
	line   int  // index into the section's lines
	cutPos int  // keeping text[:cutPos] of that line keeps every bar before this one
	first  bool // the first bar on its line
	close  bool // ends a "|: ... :|" repeat
	plain  bool // no repeat/volta/meter marks, only chord (or "%") tokens
	chords []string
}

// sectionBars splits a section's grid lines into bars, following
// internal/song's parseGrid rules for what counts as a bar.
func sectionBars(sec *srcSection) []gridBar {
	var bars []gridBar
	for li, l := range sec.lines {
		if l.kind != lkGrid {
			continue
		}
		text := l.text
		off := len(text) - len(strings.TrimLeft(text, " \t"))
		if strings.HasPrefix(text[off:], "grid:") {
			off += len("grid:")
		}
		spec := text[off:]
		segStart, sep, firstOnLine := 0, -1, true
		for i := 0; i <= len(spec); i++ {
			if i < len(spec) && spec[i] != '|' {
				continue
			}
			if b, ok := parseBarSegment(spec[segStart:i]); ok {
				b.line = li
				b.cutPos = off
				if sep >= 0 {
					b.cutPos = off + sep + 1
				}
				b.first = firstOnLine
				firstOnLine = false
				bars = append(bars, b)
			}
			sep, segStart = i, i+1
		}
	}
	return bars
}

func parseBarSegment(seg string) (gridBar, bool) {
	tok := strings.TrimSpace(seg)
	if tok == "" {
		return gridBar{}, false
	}
	opens := strings.HasPrefix(tok, ":")
	closes := strings.HasSuffix(tok, ":")
	if opens {
		tok = strings.TrimLeft(tok[1:], "0123456789")
		tok = strings.TrimSpace(tok)
	}
	if closes && len(tok) > 0 {
		tok = strings.TrimSpace(tok[:len(tok)-1])
	}
	marked := opens || closes
	if strings.HasPrefix(tok, "{") {
		if e := strings.Index(tok, "}"); e >= 0 {
			tok, marked = strings.TrimSpace(tok[e+1:]), true
		}
	}
	if strings.HasPrefix(tok, "(") {
		if e := strings.Index(tok, ")"); e >= 0 {
			tok, marked = strings.TrimSpace(tok[e+1:]), true
		}
	}
	if tok == "" && !opens && !closes {
		return gridBar{}, false
	}
	b := gridBar{close: closes, plain: !marked && tok != ""}
	for _, f := range strings.Fields(tok) {
		ch := song.TokenChord(f)
		switch {
		case ch == "%" || ch == "." || ch == "/":
		case ch != "" && ch != "N.C." && looksLikeChord(ch):
			b.chords = append(b.chords, ch)
		default:
			b.plain = false
		}
	}
	return b, true
}

// minDictionaryChords and minDictionaryDistinct define a "chords used"
// dictionary swallowed into a grid: at least this many chords, at least
// this share of them distinct.
const (
	minDictionaryChords   = 12
	minDictionaryDistinct = 0.75
)

// cutChordDictionaries cuts a chord dictionary off the end of a grid: a
// run of plain bars ending the grid, starting right after its last
// repeat close (any section) or anywhere in the song's last grid, holding
// at least minDictionaryChords chords, mostly distinct, none of which
// appears in any other bar of the song. The longest such run is cut.
func cutChordDictionaries(doc *srcDoc) []string {
	all := make([][]gridBar, len(doc.secs))
	global := map[string]int{}
	lastGridded := -1
	for i := range doc.secs {
		all[i] = sectionBars(&doc.secs[i])
		for _, b := range all[i] {
			for _, c := range b.chords {
				global[c]++
			}
		}
		if len(all[i]) > 0 {
			lastGridded = i
		}
	}
	var notes []string
	for si := range doc.secs {
		bars := all[si]
		n := len(bars)
		if n == 0 {
			continue
		}
		var cands []int
		if si == lastGridded {
			for k := 0; k < n; k++ {
				cands = append(cands, k)
			}
		} else {
			for i := n - 1; i >= 0; i-- {
				if bars[i].close {
					if i+1 < n {
						cands = append(cands, i+1)
					}
					break
				}
			}
		}
		for _, k := range cands {
			count, ok := dictionaryTail(bars[k:], global)
			if !ok {
				continue
			}
			sec := &doc.secs[si]
			start := bars[k]
			var lines []srcLine
			for li, l := range sec.lines {
				switch {
				case li < start.line || l.kind != lkGrid:
					lines = append(lines, l)
				case li == start.line && !start.first:
					l.text = strings.TrimRight(l.text[:start.cutPos], " \t")
					lines = append(lines, l)
				}
			}
			sec.lines = lines
			notes = append(notes, fmt.Sprintf("%s: cut a %d-chord list from the end of the grid (looked like a chords-used dictionary)", strings.TrimSpace(sec.label), count))
			break
		}
	}
	return notes
}

// dictionaryTail reports whether tail qualifies as a chord dictionary
// and how many chords it holds.
func dictionaryTail(tail []gridBar, global map[string]int) (int, bool) {
	local := map[string]int{}
	total := 0
	for _, b := range tail {
		if !b.plain {
			return 0, false
		}
		for _, c := range b.chords {
			local[c]++
			total++
		}
	}
	if total < minDictionaryChords || float64(len(local)) < minDictionaryDistinct*float64(total) {
		return 0, false
	}
	for c, n := range local {
		if global[c] > n {
			return 0, false
		}
	}
	return total, true
}

// dropEmptySections removes sections that cleaning emptied (hadLines[i]
// says section i had lines before). A section that was already bare is a
// structure marker in this app's charts ("chords continue", a reference
// to an earlier section) and is never dropped; neither are emptied
// "(repeat)" sections, Solo sections (solo over earlier changes) and
// references to an earlier section with the same label and real content,
// which the viewer gives meaning to.
func dropEmptySections(doc *srcDoc, hadLines []bool) []string {
	var notes []string
	contentful := map[string]bool{}
	var kept []srcSection
	for i, sec := range doc.secs {
		key := strings.ToLower(strings.TrimSpace(sec.label))
		empty, content := true, false
		for _, l := range sec.lines {
			if l.kind != lkBlank {
				empty = false
			}
			if l.kind == lkGrid || l.kind == lkLyric {
				content = true
			}
		}
		if empty && hadLines[i] && !sec.repeat && !strings.HasPrefix(key, "solo") && !contentful[key] {
			notes = append(notes, fmt.Sprintf("dropped empty section %s", strings.TrimSpace(sec.label)))
			continue
		}
		if content {
			contentful[key] = true
		}
		kept = append(kept, sec)
	}
	doc.secs = kept
	return notes
}
