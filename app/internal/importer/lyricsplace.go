package importer

import (
	"regexp"
	"strings"
)

// Stanza is one block of fetched lyrics. Label is "" when the source has
// none; see Kind.
type Stanza struct {
	Label string
	Lines []string
}

// Kind is the stanza's section kind derived from its label (see
// SectionKind): "" for an unlabelled stanza, "other" for a label that
// names no known kind.
func (s Stanza) Kind() string {
	if strings.TrimSpace(s.Label) == "" {
		return ""
	}
	if k := SectionKind(s.Label); k != "" {
		return k
	}
	return "other"
}

// Placement says what PlaceLyrics did with one section of the chart.
type Placement struct {
	Section string // the chart's section label, e.g. "Verse 2"
	Stanza  int    // index into stanzas, -1 if none
	Status  string // "placed", "reused", "kept", "missing", "instrumental"
}

// Placement statuses.
const (
	PlacePlaced       = "placed"       // the stanza was written into the section
	PlaceReused       = "reused"       // a stanza already used earlier (a repeated chorus); for a bare reference or "(repeat)" section nothing is written, the viewer repeats the earlier one
	PlaceKept         = "kept"         // the section already had lyrics and replace was false (or there was no stanza to replace them with)
	PlaceMissing      = "missing"      // no stanza for a section that expects lyrics
	PlaceInstrumental = "instrumental" // Intro/Outro/Solo/Interlude with no stanza of its own
)

// kindWords maps a label word (lowercase, accents folded) to the section
// kind it names. "pre"/"post" only count next to "chorus"/"refrain".
var kindWords = map[string]string{
	"verse": "verse", "verses": "verse", "couplet": "verse", "couplets": "verse", "strophe": "verse", "verso": "verse",
	"chorus": "chorus", "refrain": "chorus", "hook": "chorus", "choeur": "chorus", "estribillo": "chorus", "coro": "chorus",
	"prechorus": "pre-chorus", "prerefrain": "pre-chorus",
	"postchorus": "post-chorus",
	"bridge":     "bridge", "pont": "bridge", "middle": "bridge", "puente": "bridge",
	"intro": "intro",
	"outro": "outro", "coda": "outro", "ending": "outro", "fin": "outro",
	"solo":      "solo",
	"interlude": "interlude", "instrumental": "interlude", "break": "interlude", "riff": "interlude",
}

// labelFillers are words a section label may carry besides its kind
// ("Guitar Solo", "Final Chorus", "Middle 8", "Chorus x2").
var labelFillers = map[string]bool{
	"pre": true, "post": true, "guitar": true, "piano": true, "keys": true, "bass": true, "sax": true,
	"drum": true, "drums": true, "vocal": true, "vocals": true, "lead": true, "final": true, "last": true,
	"first": true, "second": true, "third": true, "double": true, "half": true, "repeat": true,
	"the": true, "and": true, "x": true, "bis": true, "eight": true, "part": true,
	"i": true, "ii": true, "iii": true, "iv": true, "v": true,
}

var accentFolder = strings.NewReplacer(
	"é", "e", "è", "e", "ê", "e", "ë", "e", "à", "a", "â", "a", "ä", "a", "á", "a",
	"ç", "c", "î", "i", "ï", "i", "í", "i", "ô", "o", "ö", "o", "ó", "o",
	"û", "u", "ù", "u", "ü", "u", "ú", "u", "œ", "oe", "ñ", "n",
)

// labelTokens splits a label into lowercase, accent-folded words and
// numbers: "Pré-refrain 2" -> [pre refrain 2], "Verse1" -> [verse 1].
func labelTokens(label string) []string {
	s := accentFolder.Replace(strings.ToLower(label))
	var toks []string
	var b strings.Builder
	digit := false
	flush := func() {
		if b.Len() > 0 {
			toks = append(toks, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			if digit {
				flush()
			}
			b.WriteRune(r)
			digit = false
		case r >= '0' && r <= '9':
			if !digit {
				flush()
			}
			b.WriteRune(r)
			digit = true
		default:
			flush()
		}
	}
	flush()
	return toks
}

// SectionKind names the kind of section a label is, English or French:
// "verse" (Verse, Couplet), "chorus" (Chorus, Refrain), "pre-chorus"
// (Pre-Chorus, Pré-refrain), "post-chorus", "bridge" (Bridge, Pont),
// "intro", "outro", "solo", "interlude" (Interlude, Instrumental, Break),
// or "" when it names none of these.
func SectionKind(label string) string {
	toks := labelTokens(label)
	has := map[string]bool{}
	for i, t := range toks {
		has[t] = true
		if i > 0 && (t == "chorus" || t == "refrain") {
			switch toks[i-1] {
			case "pre":
				has["prechorus"] = true
			case "post":
				has["postchorus"] = true
			}
		}
	}
	for _, k := range []struct {
		kind  string
		words []string
	}{
		{"pre-chorus", []string{"prechorus", "prerefrain"}},
		{"post-chorus", []string{"postchorus"}},
		{"solo", []string{"solo"}},
		{"interlude", []string{"interlude", "instrumental", "break", "riff"}},
		{"intro", []string{"intro"}},
		{"outro", []string{"outro", "coda", "ending", "fin"}},
		{"chorus", []string{"chorus", "refrain", "hook", "choeur", "estribillo", "coro"}},
		{"verse", []string{"verse", "verses", "couplet", "couplets", "strophe", "verso"}},
		{"bridge", []string{"bridge", "pont", "middle", "puente"}},
	} {
		for _, w := range k.words {
			if has[w] {
				return k.kind
			}
		}
	}
	return ""
}

// strictLabel reports whether s, unbracketed, is nothing but a section
// label: every word a kind word, a filler or a number, and SectionKind
// finds a kind. "Couplet 2", "Guitar Solo", "Chorus x2" are; "I sing the
// chorus" is not.
func strictLabel(s string) bool {
	toks := labelTokens(s)
	if len(toks) == 0 || len(toks) > 5 {
		return false
	}
	for _, t := range toks {
		if kindWords[t] == "" && !labelFillers[t] && strings.Trim(t, "0123456789") != "" {
			return false
		}
	}
	return SectionKind(s) != ""
}

// matchKey is how a section and a stanza are matched: their kind, or for
// a label with no known kind, its words ("A", "Hook 2" -> "other:...").
func matchKey(label string) string {
	if k := SectionKind(label); k != "" {
		return k
	}
	var words []string
	for _, t := range labelTokens(label) {
		if strings.Trim(t, "0123456789") != "" {
			words = append(words, t)
		}
	}
	if len(words) == 0 {
		return ""
	}
	return "other:" + strings.Join(words, " ")
}

// stanzaBracketRE is a whole-line "[Label]" or "[Verse 1: Someone]".
var stanzaBracketRE = regexp.MustCompile(`^\[([^\[\]]+)\]$`)

// stanzaLabel recognizes a label line in fetched lyrics. rest is lyric
// text following "Label:" on the same line.
func stanzaLabel(t string) (label, rest string, ok bool) {
	if m := stanzaBracketRE.FindStringSubmatch(t); m != nil {
		inner := strings.TrimSpace(m[1])
		if looksLikeChord(inner) && SectionKind(inner) == "" {
			return "", "", false
		}
		if _, isRepeat := standaloneRepeat(inner); isRepeat {
			return "", "", false
		}
		if i := strings.IndexByte(inner, ':'); i > 0 {
			inner = strings.TrimSpace(inner[:i])
		}
		if s, ok := stripRepeatTail(inner); ok {
			inner = s
		}
		return inner, "", inner != ""
	}
	if len(t) >= 2 && t[0] == '(' && t[len(t)-1] == ')' {
		inner := unwrapLabel(t)
		if strictLabel(inner) {
			return inner, "", true
		}
		return "", "", false
	}
	if i := strings.IndexByte(t, ':'); i > 0 {
		head := unwrapLabel(t[:i])
		if strictLabel(head) {
			return head, strings.TrimSpace(t[i+1:]), true
		}
		return "", "", false
	}
	if inner := unwrapLabel(t); strictLabel(inner) {
		return inner, "", true
	}
	return "", "", false
}

// stanzaChordRE is a chord marker inside a lyric line.
var stanzaChordRE = regexp.MustCompile(`\[([^\[\]]*)\]`)

// cleanStanzaLine reduces one fetched line to lyric text, or "" when the
// line carries none (chords, tab, noise, a repeat mark).
func cleanStanzaLine(t string) string {
	t = strings.TrimSpace(t)
	if t == "" || isTabStaff(t) || isNoiseLine(t) {
		return ""
	}
	if _, ok := capoValue(t); ok {
		return ""
	}
	if _, ok := standaloneRepeat(t); ok {
		return ""
	}
	if _, _, ok := chordOnlyLine(t); ok {
		return ""
	}
	t = stanzaChordRE.ReplaceAllStringFunc(t, func(m string) string {
		if inner := m[1 : len(m)-1]; inner != "" && looksLikeChord(inner) {
			return " "
		}
		return m
	})
	if s, ok := stripRepeatTail(t); ok {
		t = s
	}
	return strings.Join(strings.Fields(t), " ")
}

// SplitStanzas turns fetched lyrics (plain text with optional
// [Verse]/[Chorus]/"Refrain:"/"Couplet 1" labels, French or English) into
// stanzas: blank-line separated, inline [Chord] markers and chord-only
// lines removed, noise lines dropped. A label line starts a new stanza;
// a label with nothing under it is dropped.
func SplitStanzas(text string) []Stanza {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []Stanza
	var cur Stanza
	flush := func() {
		if len(cur.Lines) > 0 {
			out = append(out, cur)
		}
		cur = Stanza{}
	}
	for _, raw := range strings.Split(text, "\n") {
		t := strings.TrimSpace(raw)
		if t == "" {
			if len(cur.Lines) > 0 {
				flush()
			}
			continue
		}
		if label, rest, ok := stanzaLabel(t); ok {
			if len(cur.Lines) > 0 {
				flush()
			}
			cur.Label = label
			if l := cleanStanzaLine(rest); l != "" {
				cur.Lines = append(cur.Lines, l)
			}
			continue
		}
		if l := cleanStanzaLine(t); l != "" {
			cur.Lines = append(cur.Lines, l)
		}
	}
	flush()
	return out
}

// stanzaText is a stanza's lyrics normalized for comparing repeats.
func stanzaText(s Stanza) string {
	var words []string
	for _, l := range s.Lines {
		words = append(words, normalizeWords(l)...)
	}
	return strings.Join(words, " ")
}

// instrumentalKinds get lyrics only from a stanza labelled with their kind.
var instrumentalKinds = map[string]bool{"intro": true, "outro": true, "solo": true, "interlude": true}

// reusableKinds reuse their last stanza when the chart has more such
// sections than the lyrics have stanzas.
var reusableKinds = map[string]bool{"pre-chorus": true, "post-chorus": true, "bridge": true}

// PlaceLyrics writes stanzas into body's sections, matching by label kind
// (see SectionKind) and order: the n-th verse gets the n-th verse stanza;
// every chorus gets the chorus stanza (the first labelled one, or the
// most repeated text when the source is unlabelled); pre-chorus, bridge
// and post-chorus reuse their last stanza when they run out; unlabelled
// stanzas fill the remaining lyric sections in order. Intro, Outro, Solo
// and Interlude sections with no stanza of their own stay empty.
//
// Existing lyrics are kept unless replace (a section that keeps its
// lyrics still uses up its stanza, so later verses stay in step). A
// "(repeat)" section, or a bare chorus that repeats an earlier one, gets
// nothing written: the viewer already repeats the earlier words. Lyrics
// go right after the section's grid line(s), one line per lyric line, no
// chords. Grid lines, headers, comments, directives and section order are
// untouched.
//
// It returns the new body, one Placement per section, and the indices of
// stanzas used nowhere (a repeat of the chorus counts as used).
func PlaceLyrics(body string, stanzas []Stanza, replace bool) (string, []Placement, []int) {
	doc := scanDoc(body)
	n := len(stanzas)
	keys := make([]string, n)
	texts := make([]string, n)
	for i, s := range stanzas {
		if strings.TrimSpace(s.Label) != "" {
			keys[i] = matchKey(s.Label)
		}
		texts[i] = stanzaText(s)
	}

	used := make([]bool, n)
	chorus := -1
	for i := range stanzas {
		if keys[i] == "chorus" {
			chorus = i
			break
		}
	}
	if chorus < 0 {
		count := map[string]int{}
		for i := range stanzas {
			if keys[i] == "" && texts[i] != "" {
				count[texts[i]]++
			}
		}
		best := 1
		for i := range stanzas {
			if keys[i] == "" && count[texts[i]] > best {
				chorus, best = i, count[texts[i]]
			}
		}
	}
	if chorus >= 0 {
		for i := range stanzas {
			if (keys[i] == "" || keys[i] == "chorus") && texts[i] == texts[chorus] {
				used[i] = true // the chorus and its repeats
			}
		}
		used[chorus] = false
	}

	queues := map[string][]int{}
	var unlabelled []int
	for i := range stanzas {
		switch {
		case i == chorus || (chorus >= 0 && used[i]):
		case keys[i] == "":
			unlabelled = append(unlabelled, i)
		default:
			queues[keys[i]] = append(queues[keys[i]], i)
		}
	}
	pop := func(key string) int {
		if q := queues[key]; len(q) > 0 {
			queues[key] = q[1:]
			return q[0]
		}
		return -1
	}
	popUnlabelled := func() int {
		if len(unlabelled) > 0 {
			i := unlabelled[0]
			unlabelled = unlabelled[1:]
			return i
		}
		return -1
	}

	lastOf := map[string]int{}
	byLabel := map[string]int{} // stanza last given to a label, for "(repeat)" sections
	contentful := map[string]bool{}
	// The grid lines of the first section with each label: a bare
	// repeat of it inherits them in the viewer, but once it holds
	// lyrics of its own it no longer does (its chords would come from
	// whatever grid precedes it), so they're written in with the lyrics.
	gridOf := map[string][]srcLine{}
	chorusWritten := false
	placements := make([]Placement, 0, len(doc.secs))

	for si := range doc.secs {
		sec := &doc.secs[si]
		label := strings.TrimSpace(sec.label)
		lkey := strings.ToLower(label)
		p := Placement{Section: label, Stanza: -1}
		hasGrid, hasLyrics := false, false
		for _, l := range sec.lines {
			switch l.kind {
			case lkGrid:
				hasGrid = true
			case lkLyric:
				hasLyrics = true
			}
		}
		bareRef := !hasGrid && !hasLyrics && contentful[lkey]
		if hasGrid && gridOf[lkey] == nil {
			for _, l := range sec.lines {
				if l.kind == lkGrid {
					gridOf[lkey] = append(gridOf[lkey], l)
				}
			}
		}

		if sec.repeat {
			p.Status = PlaceReused
			if idx, ok := byLabel[lkey]; ok {
				p.Stanza = idx
			}
			placements = append(placements, p)
			continue
		}

		kind := SectionKind(label)
		idx, reused := -1, false
		switch {
		case kind == "chorus":
			if chorus >= 0 {
				idx, reused = chorus, chorusWritten
			} else {
				idx = popUnlabelled()
			}
		case kind == "verse":
			if idx = pop("verse"); idx < 0 {
				idx = popUnlabelled()
			}
		case reusableKinds[kind]:
			if idx = pop(kind); idx < 0 {
				if last, ok := lastOf[kind]; ok {
					idx, reused = last, true
				} else {
					idx = popUnlabelled()
				}
			}
		case instrumentalKinds[kind]:
			idx = pop(kind)
		default:
			if key := matchKey(label); key != "" {
				idx = pop(key)
			}
			if idx < 0 {
				idx = popUnlabelled()
			}
		}
		if idx >= 0 {
			used[idx] = true
			if kind != "" {
				lastOf[kind] = idx
			}
			byLabel[lkey] = idx
		}
		p.Stanza = idx

		switch {
		case kind == "chorus" && idx == chorus && chorus >= 0 && bareRef:
			p.Status = PlaceReused
		case hasLyrics && (!replace || idx < 0):
			p.Status = PlaceKept
		case idx < 0 && instrumentalKinds[kind]:
			p.Status = PlaceInstrumental
		case idx < 0:
			p.Status = PlaceMissing
		default:
			if g := gridOf[lkey]; !hasGrid && g != nil {
				sec.lines = append(append([]srcLine{}, g...), sec.lines...)
				hasGrid = true
			}
			writeLyrics(sec, stanzas[idx].Lines)
			hasLyrics = true
			p.Status = PlacePlaced
			if reused {
				p.Status = PlaceReused
			}
			if idx == chorus {
				chorusWritten = true
			}
		}
		if hasGrid || hasLyrics {
			contentful[lkey] = true
		}
		placements = append(placements, p)
	}

	var unused []int
	for i := range stanzas {
		if !used[i] {
			unused = append(unused, i)
		}
	}
	return doc.render(), placements, unused
}

// writeLyrics replaces a section's lyric lines with lines: right after
// its last grid line, else where the old lyrics started, else after the
// header and any directive lines right below it.
func writeLyrics(sec *srcSection, lines []string) {
	var add []srcLine
	for _, l := range lines {
		if s := sanitizeLyric(l); s != "" {
			add = append(add, srcLine{text: s, kind: lkLyric})
		}
	}
	var kept []srcLine
	at, lastGrid, firstLyric := -1, -1, -1
	for _, l := range sec.lines {
		if l.kind == lkLyric {
			if firstLyric < 0 {
				firstLyric = len(kept)
			}
			continue
		}
		if l.kind == lkGrid {
			lastGrid = len(kept)
		}
		kept = append(kept, l)
	}
	switch {
	case lastGrid >= 0:
		at = lastGrid + 1
	case firstLyric >= 0:
		at = firstLyric
	default:
		at = 0
		for at < len(kept) && kept[at].kind == lkDirective {
			at++
		}
	}
	out := append([]srcLine(nil), kept[:at]...)
	out = append(out, add...)
	out = append(out, kept[at:]...)
	// Removing old lyrics may leave two blank lines in a row.
	var res []srcLine
	for _, l := range out {
		if l.kind == lkBlank && len(res) > 0 && res[len(res)-1].kind == lkBlank {
			continue
		}
		res = append(res, l)
	}
	sec.lines = res
}

// sanitizeLyric makes a lyric line read back as a lyric line: no
// brackets (the parser reads them as chords), no leading "#" or "|", no
// lowercase directive prefix.
func sanitizeLyric(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(strings.TrimLeft(s, "#|"))
	s = strings.NewReplacer("[", "(", "]", ")").Replace(s)
	for _, p := range []string{"time:", "grid:", "bars:", "times:", "note:", "score:", "gp:", "chord:"} {
		if strings.HasPrefix(s, p) {
			s = strings.ToUpper(s[:1]) + s[1:]
			break
		}
	}
	return s
}
