// Package lilychord converts a LilyPond \chordmode block (the notation
// source some of the legacy MySongs/Trio/Mercy/"Other Lies" .tex songs
// use for their PRINTED SCORE, with no \[Chord] markup anywhere in the
// .tex itself) into this app's own internal/song.Measure grid — the only
// way to recover real chord data for a song whose chords were authored
// exclusively as LilyPond notation.
//
// Scope is deliberately narrow: the real corpus's .ly files are short,
// hand-written, and use a small, consistent subset of chordmode syntax
// (surveyed directly from every real file under MySongs/Trio/Mercy before
// writing this). Anything outside that subset is skipped rather than
// guessed at — a missing bar is a visibly smaller grid, not silently
// wrong data.
package lilychord

import (
	"fmt"
	"strconv"
	"strings"

	"app/internal/song"
)

// pitchClass maps a LilyPond Dutch-ish note letter to a semitone 0-11.
// The real corpus's bare "b" means B NATURAL (not Dutch solfège's B-flat)
// — confirmed against Hotel California's own \chordmode ("b1:m fis a e"
// is literally Bm F# A E, the song's real, well-known chords) and against
// sibling .tex files that spell the same chord with real letters (Trio/
// master-blaster.tex's "\[Bb]"/"\[Ab]" next to its verse.ly's "bes"/
// "aes" — "es" is the flat suffix, confirming bare letters are natural).
var pitchClass = map[byte]int{'c': 0, 'd': 2, 'e': 4, 'f': 5, 'g': 7, 'a': 9, 'b': 11}

// noteRE matches one LilyPond pitch spelling: letter + optional is/es
// accidental suffix (doubled for isis/eses, not seen in the real corpus
// but harmless to accept) — no octave marks, since none of the real
// corpus's chordmode blocks use them (chord-only staves, not real voices).
func parsePitch(s string) (semitone int, rest string, ok bool) {
	if s == "" {
		return 0, s, false
	}
	letter := s[0] | 0x20
	pc, isLetter := pitchClass[letter]
	if !isLetter {
		return 0, s, false
	}
	rest = s[1:]
	for {
		switch {
		case strings.HasPrefix(rest, "isis"):
			pc += 2
			rest = rest[4:]
		case strings.HasPrefix(rest, "eses"):
			pc -= 2
			rest = rest[4:]
		case strings.HasPrefix(rest, "is"):
			pc++
			rest = rest[2:]
		case strings.HasPrefix(rest, "es"):
			pc--
			rest = rest[2:]
		default:
			return ((pc % 12) + 12) % 12, rest, true
		}
	}
}

var sharpNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
var flatNames = [12]string{"C", "Db", "D", "Eb", "E", "F", "Gb", "G", "Ab", "A", "Bb", "B"}

func spellPitch(pc int, flat bool) string {
	if flat {
		return flatNames[pc]
	}
	return sharpNames[pc]
}

// chord is one parsed chordmode token, pitch-class only — duration and
// bar-placement are tracked separately by the measure builder below.
type chord struct {
	plain string // "" unless this is a non-chord token (a rest) — plain holds its text ("%" etc. never appear in chordmode; only rests do)
	root  int
	qual  string // this app's own quality spelling, e.g. "m7", "maj7", "9", "sus4", "" for a bare major triad
	bass  int
	hasBass bool
}

// qualityRE-free manual parser: chordmode quality suffixes are a dot-
// separated list of "step[+-]" additions after a leading keyword (m/min,
// aug, dim) — see parseQuality's own doc comment for the exact mapping
// this handles, drawn from the real corpus's actual suffixes only.
func parseQuality(suf string) string {
	if suf == "" {
		return ""
	}
	parts := strings.Split(suf, ".")
	var b strings.Builder
	first := parts[0]
	switch {
	case first == "m" || first == "min":
		b.WriteString("m")
		parts = parts[1:]
	case first == "maj":
		b.WriteString("maj")
		parts = parts[1:]
	case first == "aug":
		b.WriteString("aug")
		parts = parts[1:]
	case first == "dim":
		b.WriteString("dim")
		parts = parts[1:]
	}
	for _, p := range parts {
		if p == "" {
			continue
		}
		alt := ""
		if strings.HasSuffix(p, "+") {
			alt, p = "#", p[:len(p)-1]
		} else if strings.HasSuffix(p, "-") {
			alt, p = "b", p[:len(p)-1]
		}
		// "maj7" written as chordmode's own "maj.7" dot-form (vs the
		// single-keyword "maj7" seen directly in some files) — both
		// forms appear in the real corpus.
		if b.String() == "maj" && p == "7" && alt == "" {
			b.Reset()
			b.WriteString("maj7")
			continue
		}
		b.WriteString(alt)
		b.WriteString(p)
	}
	q := b.String()
	// m7.5- is chordmode's half-diminished spelling (minor 7, flat 5) —
	// this app's own vocabulary (confirmed against real existing grid:
	// data) spells that "m7b5", not "m7.b5"/"mb5".
	q = strings.ReplaceAll(q, "mb5", "m7b5")
	if q == "mb5" {
		q = "m7b5"
	}
	return q
}

// parseToken parses one chordmode token: PITCH[DURATION][:QUALITY][/BASS].
// "r" (a rest, with an optional duration) is returned as plain="r".
func parseToken(tok string) (c chord, duration string, ok bool) {
	if tok == "" {
		return chord{}, "", false
	}
	if tok[0] == 'r' {
		rest := tok[1:]
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		return chord{plain: "r"}, rest[:i], true
	}
	body := tok
	bass := ""
	if i := strings.IndexByte(body, '/'); i >= 0 {
		bass = body[i+1:]
		body = body[:i]
	}
	qual := ""
	if i := strings.IndexByte(body, ':'); i >= 0 {
		qual = body[i+1:]
		body = body[:i]
	}
	root, numRest, okRoot := parsePitch(body)
	if !okRoot {
		return chord{}, "", false
	}
	i := 0
	for i < len(numRest) && numRest[i] >= '0' && numRest[i] <= '9' {
		i++
	}
	duration = numRest[:i]
	c = chord{root: root, qual: parseQuality(qual)}
	if bass != "" {
		bp, _, okBass := parsePitch(bass)
		if okBass {
			c.bass = bp
			c.hasBass = true
		}
	}
	return c, duration, true
}

// durationBeats converts a LilyPond duration digit string ("1","2","4",
// "8", ...) to a count of quarter-note beats — 4/4 is assumed throughout
// (none of the real corpus's chordmode blocks carry a \time statement).
func durationBeats(d string) float64 {
	n, err := strconv.Atoi(d)
	if err != nil || n <= 0 {
		return 1
	}
	return 4.0 / float64(n)
}

// Parse converts one LilyPond \chordmode block's raw source (an entire
// .ly file's text — \include/\new ChordNames/\tempo/\markup wrapping is
// skipped automatically) into this app's Measure grid. preferFlat picks
// accidental spelling when the semitone is ambiguous (used for any chord
// whose root was shifted by a \transpose, which loses its own original
// is/es spelling) — Parse infers it itself from majority es/is usage in
// the source when both are present, so callers normally just pass false.
func Parse(src string) ([]song.Measure, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	return build(toks)
}

type tkind int

const (
	tChord tkind = iota
	tOpenBrace
	tCloseBrace
	tRepeat
	tAlternative
	tVolta
	tTranspose
	tBreak
)

type tok struct {
	kind tkind
	text string // the raw chord token (tChord), or the numeric arg (tRepeat/tVolta), or "from to" (tTranspose)
}

// tokenize strips every non-chordmode construct the real corpus's .ly
// files use (\include, \new ChordNames \with {...}, \tempo, \set ... =
// \markup {...} or plain, \bar "...", \markup ... outside chordmode, %
// comments) and returns the remaining \chordmode { ... } block as a flat
// token stream — \repeat/\alternative/\volta/\transpose keep their own
// following brace group as nested tOpenBrace/tCloseBrace pairs so build()
// can track nesting with a simple stack.
func tokenize(src string) ([]tok, error) {
	// Strip % line comments first — simplest to do on raw text before
	// any brace-aware scanning.
	var noComments strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if i := strings.IndexByte(line, '%'); i >= 0 {
			line = line[:i]
		}
		noComments.WriteString(line)
		noComments.WriteByte('\n')
	}
	s := noComments.String()

	// Locate \chordmode's balanced brace span — everything else in the
	// file (the \include, \new ChordNames header, a trailing standalone
	// \markup \vspace) is irrelevant to the chord data itself.
	idx := strings.Index(s, `\chordmode`)
	if idx < 0 {
		return nil, fmt.Errorf("no \\chordmode block found")
	}
	s = s[idx+len(`\chordmode`):]
	open := strings.IndexByte(s, '{')
	if open < 0 {
		return nil, fmt.Errorf("\\chordmode with no opening brace")
	}
	depth := 0
	end := -1
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("unbalanced \\chordmode block")
	}
	body := s[open+1 : end]

	var out []tok
	words := splitWords(body)
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case w == "":
			continue
		case w == "{":
			out = append(out, tok{kind: tOpenBrace})
		case w == "}":
			out = append(out, tok{kind: tCloseBrace})
		case w == `\break`:
			out = append(out, tok{kind: tBreak})
		case w == `\repeat`:
			// "\repeat volta N {" — the builder pushes this construct's
			// own stack frame directly from the tRepeat event, so its
			// opening brace is swallowed here rather than also being
			// emitted as a separate tOpenBrace (which would push a
			// second, bookkeeping-only frame and desync every later
			// close-brace against the WRONG frame for the rest of the
			// file — caught by a real test fixture, not by inspection:
			// Close silently never ended up true anywhere.
			if i+2 < len(words) && words[i+1] == "volta" {
				out = append(out, tok{kind: tRepeat, text: words[i+2]})
				i += 2
				i = swallowBrace(words, i)
			}
		case w == `\alternative`:
			out = append(out, tok{kind: tAlternative})
			i = swallowBrace(words, i)
		case w == `\volta`:
			if i+1 < len(words) {
				out = append(out, tok{kind: tVolta, text: words[i+1]})
				i++
				i = swallowBrace(words, i)
			}
		case w == `\transpose`:
			if i+2 < len(words) {
				out = append(out, tok{kind: tTranspose, text: words[i+1] + " " + words[i+2]})
				i += 2
				i = swallowBrace(words, i)
			}
		case w == `\bar`:
			i++ // skip the following quoted string, already a single word
		case w == `\tempo`:
			// "\tempo 4 = N" — three more words.
			i += 3
		case w == `\set`:
			// "\set noChordSymbol = \markup { ... }" or "\set key = value"
			// — skip to '=' then, if a \markup {...} follows, skip its
			// balanced brace group; otherwise skip one more word.
			for i < len(words) && words[i] != "=" {
				i++
			}
			if i+1 < len(words) && words[i+1] == `\markup` {
				i++ // land on \markup
				if i+1 < len(words) && words[i+1] == "{" {
					i++ // land on {
					d := 0
					for ; i < len(words); i++ {
						if words[i] == "{" {
							d++
						} else if words[i] == "}" {
							d--
							if d == 0 {
								break
							}
						}
					}
				}
			} else {
				i++
			}
		default:
			out = append(out, tok{kind: tChord, text: w})
		}
	}
	return out, nil
}

// swallowBrace advances past words[i+1] if it's "{" — used right after
// emitting a tRepeat/tAlternative/tVolta/tTranspose event, whose own
// stack frame the builder pushes directly from that event, not from a
// separately-tokenized tOpenBrace (see tokenize's own \repeat comment).
func swallowBrace(words []string, i int) int {
	if i+1 < len(words) && words[i+1] == "{" {
		return i + 1
	}
	return i
}

// splitWords splits on whitespace while keeping every "{"/"}" as its own
// word even when jammed against other text (chordmode source never does
// that in the real corpus, but this is cheap insurance).
func splitWords(s string) []string {
	s = strings.ReplaceAll(s, "{", " { ")
	s = strings.ReplaceAll(s, "}", " } ")
	return strings.Fields(s)
}

// stackFrame is one open "{...}" group's accounting while build() walks
// the token stream.
type stackFrame struct {
	kind        string // "repeat", "alternative", "volta", "transpose", "plain"
	repeatCount int    // "repeat" only
	transposeBy int    // "transpose" only — semitone shift, applied to every chord parsed while this frame is open (frames nest additively)
}

func build(toks []tok) ([]song.Measure, error) {
	// preferFlat: majority accidental spelling in the source, used for
	// any transposed chord (which has no original suffix of its own to
	// preserve) and, for consistency, every chord in the block.
	esCount, isCount := 0, 0
	for _, t := range toks {
		if t.kind != tChord {
			continue
		}
		if strings.Contains(t.text, "es") {
			esCount++
		}
		if strings.Contains(t.text, "is") {
			isCount++
		}
	}
	preferFlat := esCount >= isCount && esCount > 0

	var out []song.Measure
	var stack []stackFrame
	curDuration := "4"
	var curTokens []string
	curBeats := 0.0
	pendingRepeat := false     // a "repeat" frame opened but its first measure hasn't been produced yet
	pendingRepeatCount := 0    // repeatCount for the above
	var pendingVolta string    // a "volta" frame opened but its first measure hasn't been produced yet
	var repeatClose *int       // index of the measure that should receive Close=true once the repeat's whole span (including any \alternative) finishes

	transposeTotal := func() int {
		n := 0
		for _, f := range stack {
			n += f.transposeBy
		}
		return n
	}

	flushMeasure := func() {
		if len(curTokens) == 0 {
			return
		}
		m := song.Measure{Tokens: curTokens}
		if pendingRepeat {
			m.Open = true
			if pendingRepeatCount > 2 {
				m.RepeatCount = pendingRepeatCount
			}
			idx := len(out)
			repeatClose = new(int)
			*repeatClose = idx
			pendingRepeat = false
		} else if repeatClose != nil {
			*repeatClose = len(out)
		}
		if pendingVolta != "" {
			m.Volta = pendingVolta
			pendingVolta = ""
		}
		out = append(out, m)
		curTokens = nil
		curBeats = 0
	}

	for _, t := range toks {
		switch t.kind {
		case tOpenBrace:
			// A bare "{" with no preceding keyword (e.g. the extra
			// grouping brace \repeat's own body sometimes opens with) is
			// just a plain scope — push a no-op frame so tCloseBrace
			// stays balanced.
			stack = append(stack, stackFrame{kind: "plain"})
		case tCloseBrace:
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if top.kind == "repeat" {
				flushMeasure()
				if repeatClose != nil && *repeatClose < len(out) {
					out[*repeatClose].Close = true
				}
				repeatClose = nil
			}
		case tRepeat:
			n, _ := strconv.Atoi(t.text)
			stack = append(stack, stackFrame{kind: "repeat", repeatCount: n})
			pendingRepeat = true
			pendingRepeatCount = n
		case tAlternative:
			stack = append(stack, stackFrame{kind: "alternative"})
		case tVolta:
			flushMeasure() // a volta ending always starts its own fresh measure
			label := t.text + "." // e.g. "1." or "1,2,3." — same convention Measure.Volta already uses
			stack = append(stack, stackFrame{kind: "volta"})
			pendingVolta = label
		case tTranspose:
			// Deliberately NOT applied — per direct request (and
			// consistent with how this app already treats the LaTeX
			// \transpose{N} macro, see internal/latexsong's own doc
			// comment on Parse: a band plays from the LITERAL written
			// chords, with the shift noted as metadata, never baked into
			// the chord text itself). \transpose still opens/closes its
			// own stack frame — needed to keep brace nesting balanced —
			// just with no pitch effect.
			stack = append(stack, stackFrame{kind: "transpose", transposeBy: 0})
		case tBreak:
			// A layout hint only (where the engraving wraps to a new
			// printed line) — bar boundaries come from accumulated
			// duration, not \break, so there's nothing to do here beyond
			// not misreading it as a chord token.
		case tChord:
			c, dur, ok := parseToken(t.text)
			if !ok {
				continue // unrecognized token — skip rather than guess
			}
			if dur != "" {
				curDuration = dur
			}
			beats := durationBeats(curDuration)
			var label string
			if c.plain == "r" {
				// This app's grammar has no separate "silence" token —
				// a held "." (sustain-previous-chord) is the closest
				// honest equivalent a rest can map to.
				label = "."
			} else {
				shift := transposeTotal()
				root := ((c.root+shift)%12 + 12) % 12
				label = spellPitch(root, preferFlat) + c.qual
				if c.hasBass {
					bass := ((c.bass+shift)%12 + 12) % 12
					label += "/" + spellPitch(bass, preferFlat)
				}
			}
			curTokens = append(curTokens, label)
			curBeats += beats
			if curBeats >= 3.999 {
				flushMeasure()
			}
		}
	}
	flushMeasure()
	return out, nil
}
