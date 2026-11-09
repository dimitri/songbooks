package tab

import (
	"fmt"
	"strings"
)

// ParseScore parses src (a "score:" file's contents) into a Riff. It
// looks for a \new TabStaff block (the shape every real legacy riff this
// subset targets uses) anywhere in the source — inside a << ... >>
// group, inside a \repeat, alongside a \chords/\chordmode/\new ChordNames
// block (all three ignored: chord charts are "grid:"'s job, not
// "score:"'s). A sibling \new Staff/Voice carrying the same pitches
// (engraved standard notation, for sheet-music display) is recognized
// but not extracted — the TabStaff block alone has everything needed for
// tab rendering (pitch, tuning, minimumFret), so this package only
// parses that one.
func ParseScore(src string) (*Riff, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &lyParser{toks: toks}
	var riff *Riff
	for !p.atEnd() {
		found, err := p.topLevel()
		if err != nil {
			return nil, err
		}
		if found != nil {
			riff = found
		}
	}
	if riff == nil {
		return nil, fmt.Errorf("score: no \\new TabStaff block found")
	}
	return riff, nil
}

// ParseVoice parses one song.ScoreVoice's Body into a Riff — the
// label-driven shortcut form (see song.ScoreVoice's own doc comment):
// raw bare content, no "\new TabStaff { ... }"/"\new DrumStaff {
// \drummode { ... } }" boilerplate required. label picks the grammar and
// default tuning (case-insensitive, surrounding space trimmed):
// "drum"/"drums" parses raw directly as \drummode content (the same
// parseDrumModeBody a "\new DrumStaff" block's own content goes through
// — see parseDrumStaffBody); "bass" parses it as TabStaff content
// (parseTabStaffBody — bare absolute-pitch notes, or a "\relative {...}"
// wrapper, or a handful of other directives it already tolerates — see
// its own doc comment) at standard 4-string bass tuning; anything else
// (a custom name like "guitar-1", or no label at all) gets the same
// TabStaff grammar at standard 6-string guitar tuning, this package's
// existing default.
//
// If raw (trimmed) already starts with "\new", it's the full explicit
// form instead — delegated to ParseScore unchanged, so a voice that
// needs something the shortcut can't express (an unusual tuning, an
// explicit minimumFret, two staves in one voice) still has an escape
// hatch.
func ParseVoice(label, raw string) (*Riff, error) {
	if strings.HasPrefix(strings.TrimSpace(raw), `\new`) {
		return ParseScore(raw)
	}
	toks, err := lex(raw)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "drum", "drums":
		return parseDrumModeBody(toks)
	case "bass":
		return parseTabStaffBody(toks, bassTuning, 0)
	default:
		return parseTabStaffBody(toks, defaultGuitarTuning, 0)
	}
}

// NamedRiff pairs one "\new TabStaff/DrumStaff" block's extracted Riff
// with an instrument label — either that context's own explicit
// LilyPond name ("\new TabStaff = \"Bass\" { ... }", real context-naming
// syntax LilyPond already supports) or, when none is given, an inferred
// default: "drums" for a DrumStaff, "bass" for a <=4-string TabStaff,
// "guitar" for a 6-string one — numbered ("guitar-2", ...) when more
// than one of the same kind appears unlabeled in the same file. See
// ParseScoreTracks.
type NamedRiff struct {
	Name string
	Riff *Riff
}

// ParseScoreTracks is ParseScore generalized to a "score:" source
// carrying SEVERAL simultaneous instrument staves — real LilyPond syntax
// already supports this (one "<< \new TabStaff {...} \new DrumStaff
// {...} >>" group; each staff's own \with{stringTunings=...}/\relative/
// \drummode body parses exactly as it does standalone), so multi-track
// input doesn't need a second grammar: a band's whole arrangement for
// one section — drums, bass, two guitars — can live in the ONE "score:"
// field that section already has (see song_scores' own doc comment: the
// per-section editing model doesn't change, only how much a single
// score can hold). Every "\new TabStaff/DrumStaff" block found anywhere
// in src (nested inside << >> groups, same traversal ParseScore itself
// uses) is returned, in source order.
func ParseScoreTracks(src string) ([]NamedRiff, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &lyParser{toks: toks}
	for !p.atEnd() {
		if _, err := p.topLevel(); err != nil {
			return nil, err
		}
	}
	if len(p.collected) == 0 {
		return nil, fmt.Errorf("score: no \\new TabStaff or \\new DrumStaff block found")
	}
	return p.collected, nil
}

// --- Lexer -----------------------------------------------------------

type tokKind int

const (
	tWord tokKind = iota // bareword/number/pitch-duration token, or a "\command"
	tLBrace
	tRBrace
	tLDouble // <<
	tRDouble // >>
	tLAngle  // <  (chord cluster open, note mode only)
	tRAngle  // >
	tString  // "..."
)

type token struct {
	kind tokKind
	text string // for tWord: the word itself (including a leading \ for commands); for tString: the unquoted contents
}

// lex turns src into a flat token stream. It deliberately knows nothing
// about LilyPond's full grammar — just enough punctuation to let the
// parser find block boundaries, commands and note events. Slur
// parentheses ("(", ")") are deliberately NOT their own tokens: real
// files attach them directly to a note token with no space (e.g.
// "bes16(", "c'4)"), and since this subset doesn't render slurs, the
// note parser just strips them off a word token's ends.
func lex(src string) ([]token, error) {
	var toks []token
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '%':
			// Line comment, or a %{ ... %} block comment.
			if i+1 < n && src[i+1] == '{' {
				end := strings.Index(src[i+2:], "%}")
				if end < 0 {
					return nil, fmt.Errorf("score: unterminated %%{ comment")
				}
				i += 2 + end + 2
			} else {
				end := strings.IndexByte(src[i:], '\n')
				if end < 0 {
					i = n
				} else {
					i += end
				}
			}
		case c == '"':
			end := strings.IndexByte(src[i+1:], '"')
			if end < 0 {
				return nil, fmt.Errorf("score: unterminated string literal")
			}
			toks = append(toks, token{tString, src[i+1 : i+1+end]})
			i += end + 2
		case c == '{':
			toks = append(toks, token{tLBrace, "{"})
			i++
		case c == '}':
			toks = append(toks, token{tRBrace, "}"})
			i++
		case c == '<' && i+1 < n && src[i+1] == '<':
			toks = append(toks, token{tLDouble, "<<"})
			i += 2
		case c == '>' && i+1 < n && src[i+1] == '>':
			toks = append(toks, token{tRDouble, ">>"})
			i += 2
		case c == '<':
			toks = append(toks, token{tLAngle, "<"})
			i++
		case c == '>':
			toks = append(toks, token{tRAngle, ">"})
			i++
		case c == '#':
			// A Scheme literal: #t, #f, ##t, ##f, #3, #'(...), #'symbol...
			// We never evaluate these, only need to consume them as one
			// opaque word so the parser can skip them — balance parens if
			// the literal contains any, else stop at the next delimiter.
			j := i + 1
			depth := 0
			for j < n {
				switch src[j] {
				case '(':
					depth++
				case ')':
					depth--
					if depth <= 0 {
						j++
						goto schemeDone
					}
				case ' ', '\t', '\n', '\r', '{', '}', '<', '>':
					if depth == 0 {
						goto schemeDone
					}
				}
				j++
			}
		schemeDone:
			toks = append(toks, token{tWord, src[i:j]})
			i = j
		default:
			j := i
			for j < n {
				switch src[j] {
				case ' ', '\t', '\n', '\r', '{', '}', '<', '>', '"', '%', '#':
					goto wordDone
				}
				j++
			}
		wordDone:
			if j == i {
				return nil, fmt.Errorf("score: unexpected character %q", c)
			}
			toks = append(toks, token{tWord, src[i:j]})
			i = j
		}
	}
	return toks, nil
}

// --- Top-level parser --------------------------------------------------

type lyParser struct {
	toks []token
	pos  int

	// collected accumulates every "\new TabStaff/DrumStaff" riff found
	// during this parse, each tagged with its instrument name — see
	// ParseScoreTracks. ParseScore's own single-riff loop ignores this
	// (it tracks its own "last riff wins" return value), so populating it
	// unconditionally in parseNewContext costs ParseScore callers
	// nothing.
	collected []NamedRiff
	kindCount map[string]int // "guitar" -> how many unlabeled ones seen so far, for auto-numbering
}

// autoName picks a default instrument label for a "\new TabStaff/
// DrumStaff" block that didn't carry an explicit "= \"name\"" — see
// NamedRiff's own doc comment for the naming rule.
func (p *lyParser) autoName(ctx string, riff *Riff) string {
	if p.kindCount == nil {
		p.kindCount = map[string]int{}
	}
	base := "guitar"
	switch {
	case ctx == "DrumStaff":
		base = "drums"
	case len(riff.Tuning) <= 4:
		base = "bass"
	}
	p.kindCount[base]++
	if p.kindCount[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, p.kindCount[base])
}

func (p *lyParser) atEnd() bool { return p.pos >= len(p.toks) }

func (p *lyParser) peek() token {
	if p.atEnd() {
		return token{}
	}
	return p.toks[p.pos]
}

func (p *lyParser) next() token {
	t := p.peek()
	p.pos++
	return t
}

func (p *lyParser) isCmd(name string) bool {
	t := p.peek()
	return t.kind == tWord && t.text == "\\"+name
}

// skipBalanced consumes a brace-delimited block (the opener must be the
// current token) and returns its contents as a sub-slice of tokens,
// without the surrounding braces.
func (p *lyParser) skipBalanced() ([]token, error) {
	if p.peek().kind != tLBrace {
		return nil, fmt.Errorf("score: expected '{'")
	}
	p.next()
	start := p.pos
	depth := 1
	for !p.atEnd() {
		switch p.peek().kind {
		case tLBrace:
			depth++
		case tRBrace:
			depth--
			if depth == 0 {
				body := p.toks[start:p.pos]
				p.next()
				return body, nil
			}
		}
		p.next()
	}
	return nil, fmt.Errorf("score: unterminated block")
}

func (p *lyParser) skipOneArg() {
	if !p.atEnd() {
		p.next()
	}
}

// topLevel consumes one top-level statement and returns a non-nil *Riff
// if it was (or contained) the \new TabStaff block this subset targets.
func (p *lyParser) topLevel() (*Riff, error) {
	switch {
	case p.isCmd("include"):
		p.next()
		p.skipOneArg() // the included filename string
		return nil, nil
	case p.peek().kind == tLDouble:
		return p.parseGroup(tRDouble)
	case p.isCmd("repeat"):
		p.next()
		p.skipOneArg() // "volta"
		p.skipOneArg() // the repeat count
		body, err := p.skipBalanced()
		if err != nil {
			return nil, err
		}
		sub := &lyParser{toks: body}
		var riff *Riff
		for !sub.atEnd() {
			r, err := sub.topLevel()
			if err != nil {
				return nil, err
			}
			if r != nil {
				riff = r
			}
			if sub.isCmd("alternative") {
				sub.next()
				if _, err := sub.skipBalanced(); err != nil {
					return nil, err
				}
			}
		}
		return riff, nil
	case p.isCmd("new"):
		return p.parseNewContext()
	case p.isCmd("chordmode"), p.isCmd("chords"):
		// Chord charts are "grid:"'s job now, not "score:"'s — skip
		// entirely rather than extracting anything.
		p.next()
		_, err := p.skipBalanced()
		return nil, err
	case p.isCmd("markup"):
		// \markup \vspace #1, or \markup { ... } — a page-layout hint,
		// meaningless to our own layout.
		p.next()
		if p.peek().kind == tLBrace {
			_, err := p.skipBalanced()
			return nil, err
		}
		p.skipOneArg() // e.g. "\vspace"
		if !p.atEnd() && p.peek().kind != tLBrace && strings.HasPrefix(p.peek().text, "#") {
			p.skipOneArg() // the "#1" after \vspace
		}
		return nil, nil
	default:
		// Anything else at top level we don't recognize (a stray
		// \version, \paper{...}, etc.) — consume one token/block rather
		// than erroring, so copy-pasted boilerplate from a real .ly file
		// doesn't break the whole import.
		return nil, p.skipUnknownStatement()
	}
}

// parseGroup consumes a "<< ... >>" (or, via end=tRBrace, a plain brace
// block treated the same way) looking for a \new TabStaff inside.
func (p *lyParser) parseGroup(end tokKind) (*Riff, error) {
	p.next() // consume the opener
	var riff *Riff
	for p.peek().kind != end {
		if p.atEnd() {
			return nil, fmt.Errorf("score: unterminated group")
		}
		r, err := p.topLevel()
		if err != nil {
			return nil, err
		}
		if r != nil {
			riff = r
		}
	}
	p.next()
	return riff, nil
}

func (p *lyParser) skipUnknownStatement() error {
	p.next()
	if p.peek().kind == tLBrace {
		_, err := p.skipBalanced()
		return err
	}
	return nil
}

// parseNewContext handles "\new <Context> [= \"name\"] [\with {...}] <body>".
// The optional "= \"name\""  is real LilyPond context-naming syntax
// (e.g. "\new TabStaff = \"Bass\" { ... }") — this subset only reads it
// to label a multi-track score's riffs (see NamedRiff/ParseScoreTracks);
// it has no other effect here, same as real LilyPond, where it mainly
// matters for cross-referencing a context elsewhere in the file.
func (p *lyParser) parseNewContext() (*Riff, error) {
	p.next() // consume \new
	if p.peek().kind != tWord {
		return nil, fmt.Errorf("score: expected a context name after \\new")
	}
	ctx := p.next().text // "Voice", "Staff", "TabStaff", "DrumStaff", "ChordNames"

	explicitName := ""
	if p.peek().kind == tWord && p.peek().text == "=" {
		p.next()
		if p.peek().kind != tString {
			return nil, fmt.Errorf("score: expected a quoted name after '=' in \\new %s =", ctx)
		}
		explicitName = p.next().text
	}

	minFret := 0
	tuning := defaultGuitarTuning
	if p.isCmd("with") {
		p.next()
		withBody, err := p.skipBalanced()
		if err != nil {
			return nil, err
		}
		minFret, tuning = parseWithTuning(withBody, tuning)
	}

	switch p.peek().kind {
	case tLBrace:
		body, err := p.skipBalanced()
		if err != nil {
			return nil, err
		}
		var riff *Riff
		switch ctx {
		case "TabStaff":
			riff, err = parseTabStaffBody(body, tuning, minFret)
		case "DrumStaff":
			riff, err = parseDrumStaffBody(body)
		default:
			// Standard-notation Staff/Voice (and a bare ChordNames, which
			// does nothing without an attached \chordmode anyway) — not
			// this package's target; see the package doc comment.
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		name := explicitName
		if name == "" {
			name = p.autoName(ctx, riff)
		}
		p.collected = append(p.collected, NamedRiff{Name: name, Riff: riff})
		return riff, nil
	case tLDouble:
		return p.parseGroup(tRDouble)
	default:
		return nil, nil
	}
}

var defaultGuitarTuning = []int{64, 59, 55, 50, 45, 40} // E4 B3 G3 D3 A2 E2 — string 1 (highest) first

// bassTuning is LilyPond's own named #bass-tuning preset — standard
// 4-string bass, E1 A1 D2 G2 low to high — string 1 (highest) first,
// same convention as defaultGuitarTuning. Real example: this repo's
// legacy "Other Lies/2-Surfacing/06-Kawasaki/bass.ly",
// "\with { stringTunings = #bass-tuning }".
var bassTuning = []int{43, 38, 33, 28} // G2 D2 A1 E1

// namedTunings maps a LilyPond Scheme tuning-preset name (as written
// after a bare "#", e.g. "#bass-tuning") to this package's own
// string-1-first MIDI tuning list. Only the presets actually seen in
// this repo's real corpus are recognized; anything else falls back to
// defaultTuning rather than guessing at an unfamiliar Scheme value.
var namedTunings = map[string][]int{
	"bass-tuning":   bassTuning,
	"guitar-tuning": defaultGuitarTuning,
}

// parseWithTuning reads a \with{...} block's body looking only for what
// this subset needs: an explicit minimumFret (rare inside \with; usually
// set later via \set — see parseTabStaffBody) and a stringTunings value
// — either a named Scheme preset ("#bass-tuning", the only form seen in
// the real corpus) or, if unrecognized, defaultTuning unchanged rather
// than guessing at an arbitrary Scheme tuning-list expression.
func parseWithTuning(toks []token, defaultTuning []int) (minFret int, tuning []int) {
	tuning = defaultTuning
	for i, t := range toks {
		if t.kind != tWord || t.text != "stringTunings" {
			continue
		}
		for j := i + 1; j < len(toks); j++ {
			v := toks[j]
			if v.kind != tWord || !strings.HasPrefix(v.text, "#") {
				continue
			}
			name := strings.TrimPrefix(v.text, "#")
			if nt, ok := namedTunings[name]; ok {
				tuning = nt
			}
			break
		}
		break
	}
	return 0, tuning
}
