package melody

import (
	"strings"
	"unicode"
)

// Syllables: a lyric line cut into what is sung on one note each — a
// small, deterministic hyphenation by rule for French and English (no
// dictionary): vowel groups are the syllables' cores, the consonants
// between two of them split by the usual rules (one goes with the next
// vowel; two are parted unless they're a pair that starts a syllable,
// "tr", "bl", "ch"…), and the endings each language sings its own way:
// French's mute "e" counted before a consonant but not before a vowel or
// at the end of a line, English's silent final "e", "-ed" and "-es".

// Syl is one sung syllable.
type Syl struct {
	Text string
	// WordStart/WordEnd: the first/last syllable of its word (a word's
	// syllables are written joined by hyphens under the notes).
	WordStart, WordEnd bool
}

// Lang is "fr" or "en".
type Lang string

const (
	French  Lang = "fr"
	English Lang = "en"
)

// GuessLang says which of the two a song's lyrics read as: French when
// its accents and little words outnumber English's.
func GuessLang(text string) Lang {
	fr, en := 0, 0
	for _, r := range strings.ToLower(text) {
		if strings.ContainsRune("éèêàâçùûôîïœ", r) {
			fr++
		}
	}
	for _, w := range words(strings.ToLower(text)) {
		switch strings.Trim(w, "'’") {
		case "le", "la", "les", "de", "des", "et", "je", "tu", "il", "elle", "nous", "vous", "que", "qui", "dans", "pour", "pas", "une", "est", "sur", "mon", "ma", "mes", "ton", "ta", "au", "du", "ne", "plus", "c'est", "j'ai":
			fr += 2
		case "the", "and", "you", "i", "to", "of", "my", "your", "is", "in", "it", "me", "on", "we", "be", "all", "for", "with", "love", "don't", "i'm":
			en += 2
		}
	}
	if fr > en {
		return French
	}
	return English
}

func words(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '-' || r == '—' })
}

// SplitLine is a lyric line's syllables, in order. Text in parentheses is
// left out (backing vocals, an echo: not the lead's).
func SplitLine(line string, lang Lang) []Syl {
	line = dropParens(line)
	ws := strings.Fields(line)
	var out []Syl
	for wi, w := range ws {
		// "c'est-à-dire", "peut-être": each part a word, joined.
		parts := strings.Split(w, "-")
		for pi, part := range parts {
			if letters(part) == "" {
				// Punctuation on its own ("!") goes with the syllable before.
				if n := len(out); n > 0 && pi == 0 {
					out[n-1].Text += part
				}
				continue
			}
			next := ""
			if pi+1 < len(parts) {
				next = parts[pi+1]
			} else if wi+1 < len(ws) {
				next = ws[wi+1]
			}
			syls := Syllables(part, lang, next)
			for k, s := range syls {
				out = append(out, Syl{Text: s, WordStart: k == 0 && pi == 0, WordEnd: k == len(syls)-1 && pi == len(parts)-1})
			}
			if pi < len(parts)-1 && len(out) > 0 {
				// A written hyphen joins: the parts read as one word.
				out[len(out)-1].WordEnd = false
			}
		}
	}
	// A part with no letters after a hyphen leaves the word open.
	if n := len(out); n > 0 {
		out[n-1].WordEnd = true
	}
	return out
}

func dropParens(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '(' || r == '[':
			depth++
		case (r == ')' || r == ']') && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func letters(w string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return r
		}
		return -1
	}, w)
}

// Syllables cuts one word (its punctuation kept on its last syllable);
// next is the word after it in the line ("" at the line's end), for
// French's mute "e".
func Syllables(word string, lang Lang, next string) []string {
	// Leading and trailing punctuation stay outside the cut.
	rs := []rune(word)
	a, b := 0, len(rs)
	for a < b && !unicode.IsLetter(rs[a]) {
		a++
	}
	for b > a && !unicode.IsLetter(rs[b-1]) {
		b--
	}
	if a == b {
		return nil
	}
	lead, core, trail := string(rs[:a]), rs[a:b], string(rs[b:])
	var parts []string
	if lang == French {
		parts = frenchSyllables(core, next)
	} else {
		parts = englishSyllables(core)
	}
	parts[0] = lead + parts[0]
	parts[len(parts)-1] += trail
	return parts
}

func isVowelFR(r rune) bool {
	return strings.ContainsRune("aeiouyàâäéèêëîïôöùûüÿœæ", unicode.ToLower(r))
}
func isVowelEN(r rune, i int, w []rune) bool {
	l := unicode.ToLower(r)
	if strings.ContainsRune("aeiouéèêëïöü", l) {
		return true
	}
	// "y" is a vowel but at the start of a word or before a vowel.
	return l == 'y' && i > 0 && !(i+1 < len(w) && strings.ContainsRune("aeiou", unicode.ToLower(w[i+1])))
}

// nuclei are the vowel groups of w: [start, end) rune indexes.
func nuclei(w []rune, vowel func(i int) bool, split func(i int) bool) [][2]int {
	var out [][2]int
	for i := 0; i < len(w); {
		if !vowel(i) {
			i++
			continue
		}
		j := i + 1
		for j < len(w) && vowel(j) && !split(j) {
			j++
		}
		out = append(out, [2]int{i, j})
		i = j
	}
	return out
}

// cut puts the boundaries between nuclei: between two vowel groups, the
// consonants c; with one, it goes to the next syllable; with more, the
// last two go together when they start a syllable (onset), else only
// the last one.
func cut(w []rune, nuc [][2]int, onset, coda func(a, b rune) bool) []string {
	if len(nuc) <= 1 {
		return []string{string(w)}
	}
	var out []string
	start := 0
	for k := 0; k+1 < len(nuc); k++ {
		c0, c1 := nuc[k][1], nuc[k+1][0] // consonants [c0, c1)
		n := c1 - c0
		var at int
		switch {
		case n == 0:
			at = c0
		case n == 1:
			at = c0
		case coda != nil && coda(unicode.ToLower(w[c0]), unicode.ToLower(w[c0+1])):
			at = c0 + 2
		default:
			at = c1 - 1
			if onset(unicode.ToLower(w[c1-2]), unicode.ToLower(w[c1-1])) {
				at = c1 - 2
			}
		}
		out = append(out, string(w[start:at]))
		start = at
	}
	return append(out, string(w[start:]))
}

func frenchOnset(a, b rune) bool {
	switch string([]rune{a, b}) {
	case "bl", "br", "cl", "cr", "dr", "fl", "fr", "gl", "gr", "pl", "pr", "tr", "vr", "ch", "ph", "th", "gn", "qu", "gu":
		return true
	}
	// An apostrophe goes with what follows: l'a-mi.
	return b == '\'' || b == '’' || a == '\'' || a == '’'
}

func frenchSyllables(w []rune, next string) []string {
	low := []rune(strings.ToLower(string(w)))
	vowel := func(i int) bool {
		if !isVowelFR(low[i]) {
			return false
		}
		// The "u" of "qu", "gu" before a vowel is part of the consonant.
		if low[i] == 'u' && i > 0 && (low[i-1] == 'q' || low[i-1] == 'g') && i+1 < len(low) && isVowelFR(low[i+1]) {
			return false
		}
		return true
	}
	split := func(j int) bool {
		// A tréma, an "é" after a vowel or a vowel after "é" (cré-a,
		// po-è-te), "i"/"y" before a vowel after another (pa-ys stays: rare).
		r, p := low[j], low[j-1]
		return strings.ContainsRune("ïüÿ", r) || strings.ContainsRune("éè", r) || (p == 'é' && r != 'e') || (p == 'o' && r == 'ë')
	}
	nuc := nuclei(low, vowel, split)
	parts := cut(w, nuc, frenchOnset, nil)
	if len(parts) < 2 {
		return parts
	}
	// The mute "e" ending the word ("-e", "-es", "-ent" after a
	// consonant): its own syllable before a consonant inside the line,
	// none before a vowel or an "h" or at the line's end.
	last := []rune(strings.ToLower(parts[len(parts)-1]))
	mute := false
	for _, end := range []string{"e", "es", "ent"} {
		e := []rune(end)
		if len(last) >= len(e)+1 && string(last[len(last)-len(e):]) == end && !isVowelFR(last[len(last)-len(e)-1]) {
			// Only when that ending is the syllable's only vowel.
			v := 0
			for _, r := range last {
				if isVowelFR(r) {
					v++
				}
			}
			mute = v == 1 && (end != "ent" || len(parts) > 1)
			break
		}
	}
	if mute && !startsWithConsonant(next) {
		parts[len(parts)-2] += parts[len(parts)-1]
		parts = parts[:len(parts)-1]
	}
	return parts
}

func startsWithConsonant(w string) bool {
	for _, r := range strings.ToLower(w) {
		if unicode.IsLetter(r) {
			return !isVowelFR(r) && r != 'h'
		}
	}
	return false
}

func englishOnset(a, b rune) bool {
	switch string([]rune{a, b}) {
	case "bl", "br", "cl", "cr", "dr", "fl", "fr", "gl", "gr", "pl", "pr", "tr", "sc", "sk", "sl", "sm", "sn", "sp", "st", "sw", "tw", "ch", "sh", "th", "ph", "wh", "wr", "qu", "kn":
		return true
	}
	return false
}

func englishSyllables(w []rune) []string {
	// A contraction's tail ("'s", "'ll", "n't") is sung with the word.
	if i := strings.LastIndexAny(string(w), "'’"); i > 0 {
		head := []rune(string(w)[:i])
		if tail := string(w)[i:]; len([]rune(tail)) <= 3 && len(head) > 1 {
			parts := englishSyllables(head)
			parts[len(parts)-1] += tail
			return parts
		}
	}
	low := []rune(strings.ToLower(string(w)))
	n := len(low)
	vowel := func(i int) bool { return isVowelEN(low[i], i, low) }
	// Endings that aren't syllables: a silent final "e" ("make", but
	// "the", "be" and "-le" after a consonant are sung), "-ed" but after
	// t/d, "-es" but after s, x, z, ch, sh, ce, ge.
	silent := -1
	switch {
	case n >= 3 && low[n-1] == 'e' && !vowel(n-2) && !(low[n-2] == 'l' && !vowel(n-3)):
		silent = n - 1
	case n >= 4 && low[n-2] == 'e' && low[n-1] == 'd' && low[n-3] != 't' && low[n-3] != 'd' && !vowel(n-3):
		silent = n - 2
	case n >= 4 && low[n-2] == 'e' && low[n-1] == 's' && !vowel(n-3) && !strings.ContainsRune("sxz", low[n-3]) && !(low[n-3] == 'h' && (low[n-4] == 'c' || low[n-4] == 's')) && low[n-3] != 'c' && low[n-3] != 'g':
		silent = n - 2
	}
	isV := func(i int) bool { return i != silent && vowel(i) }
	nuc := nuclei(low, isV, func(int) bool { return false })
	if len(nuc) <= 1 {
		return []string{string(w)}
	}
	// "ng", "ck" end a syllable: sing-ing, pick-ing.
	return cut(w, nuc, englishOnset, func(a, b rune) bool {
		return (a == 'n' && b == 'g') || (a == 'c' && b == 'k') || (a == 'g' && b == 'h')
	})
}
