package tab

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// parseTabStaffBody parses one "\new TabStaff { ... }" block's contents:
// \tabFullNotation (skipped — a display directive, not content), \set
// statements (consumed for the one this subset acts on:
// TabStaff.minimumFret; anything else is parsed and ignored), and a
// \relative { ... } block, which is where the actual notes live in
// every real file this subset targets.
func parseTabStaffBody(toks []token, tuning []int, minFret int) (*Riff, error) {
	p := &lyParser{toks: toks}
	lastPitch := 60 // c', LilyPond's implicit \relative reference when none is given explicitly
	var bars []Bar
directives:
	for !p.atEnd() {
		switch {
		case p.isCmd("tabFullNotation"), p.isCmd("restrainOpenStrings"):
			p.next()
		case p.isCmd("tempo"):
			p.next()
			for i := 0; i < 3 && !p.atEnd() && p.peek().kind != tLBrace; i++ {
				p.next() // "4 = 90" — tempo is a song-level concern, not this riff's
			}
		case p.isCmd("key"):
			p.next()
			p.skipOneArg()
			p.skipOneArg()
		case p.isCmd("clef"):
			p.next()
			p.skipOneArg()
		case p.isCmd("set"):
			n, ok, err := parseMinimumFretSet(p)
			if err != nil {
				return nil, err
			}
			if ok {
				minFret = n
			}
		case p.isCmd("relative"):
			p.next()
			body, err := p.skipBalanced()
			if err != nil {
				return nil, err
			}
			newBars, err := parseRelativeMusic(body, &lastPitch, tuning, minFret, false)
			if err != nil {
				return nil, err
			}
			bars = append(bars, newBars...)
		default:
			// A bare note/chord/\tuplet/\grace with no \relative wrapper
			// at all (e.g. Mercy/bad-spell/intro.ly, whose TabStaff body
			// goes straight from its \set statements into plain notes) —
			// real LilyPond's default pitch mode absent \relative is
			// ABSOLUTE, not an error. Treat everything from here to the
			// end of this TabStaff body as one absolute-pitch run.
			newBars, err := parseRelativeMusic(p.toks[p.pos:], &lastPitch, tuning, minFret, true)
			if err != nil {
				return nil, err
			}
			bars = append(bars, newBars...)
			break directives
		}
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("score: \\new TabStaff has no note content")
	}
	return &Riff{Tuning: tuning, MinimumFret: minFret, Bars: bars}, nil
}

// parseMinimumFretSet consumes one "\set X.Y = value" statement,
// returning (minimumFret, true) if it was specifically
// "TabStaff.minimumFret", else (_, false). Any other \set this subset
// doesn't act on is still consumed (so parsing can continue), just
// ignored.
func parseMinimumFretSet(p *lyParser) (int, bool, error) {
	p.next() // \set
	var path []string
	for !p.atEnd() && p.peek().kind == tWord && p.peek().text != "=" {
		path = append(path, p.next().text)
	}
	if !p.atEnd() && p.peek().text == "=" {
		p.next()
	}
	full := strings.Join(path, "")
	n, ok := 0, false
	if full == "TabStaff.minimumFret" && !p.atEnd() && p.peek().kind == tWord {
		if v, err := strconv.Atoi(strings.TrimPrefix(p.peek().text, "#")); err == nil {
			n, ok = v, true
		}
	}
	if p.isCmd("markup") {
		p.next()
	}
	if p.peek().kind == tLBrace {
		_, err := p.skipBalanced()
		return n, ok, err
	}
	if !p.atEnd() {
		p.next()
	}
	return n, ok, nil
}

// --- \relative music ---------------------------------------------------

// relParser walks one \relative{...} block's tokens, grouping events
// into bars by cumulative duration against an assumed 4/4 — none of the
// real files this subset targets ever change time signature inside a
// riff, and none write an explicit "|" either (matching how LilyPond
// itself would auto-barline this same input).
type relParser struct {
	toks []token
	pos  int

	lastPitch int
	minFret   int
	lastDur   string
	tuning    []int
	absolute  bool // true for a TabStaff body with no \relative wrapper — see parseTabStaffBody

	curBeats float64
	curBar   []Beat
	bars     []Bar

	// pendingSlideMIDI is the resolved pitch of a "\grace <note>(
	// \glissando" just consumed, awaiting the main note it slides into
	// — -1 means no pending slide. See Beat.HasSlideFrom's own doc
	// comment for the real corpus example this models.
	pendingSlideMIDI int
}

func parseRelativeMusic(toks []token, lastPitch *int, tuning []int, minFret int, absolute bool) ([]Bar, error) {
	rp := &relParser{toks: toks, lastPitch: *lastPitch, minFret: minFret, lastDur: "4", tuning: tuning, absolute: absolute, pendingSlideMIDI: -1}
	if err := rp.sequence(); err != nil {
		return nil, err
	}
	rp.flushBar()
	*lastPitch = rp.lastPitch
	return rp.bars, nil
}

func (p *relParser) atEnd() bool { return p.pos >= len(p.toks) }
func (p *relParser) peek() token {
	if p.atEnd() {
		return token{}
	}
	return p.toks[p.pos]
}
func (p *relParser) next() token {
	t := p.peek()
	p.pos++
	return t
}
func (p *relParser) isCmd(name string) bool {
	t := p.peek()
	return t.kind == tWord && t.text == "\\"+name
}

const barBeatsDefault = 4.0 // assumed 4/4

func (p *relParser) sequence() error {
	for !p.atEnd() {
		switch {
		case p.isCmd("set"):
			p.next()
			var path []string
			for !p.atEnd() && p.peek().kind == tWord && p.peek().text != "=" {
				path = append(path, p.next().text)
			}
			if !p.atEnd() && p.peek().text == "=" {
				p.next()
			}
			if strings.Join(path, "") == "TabStaff.minimumFret" && !p.atEnd() {
				if n, err := strconv.Atoi(strings.TrimPrefix(p.peek().text, "#")); err == nil {
					p.minFret = n
				}
			}
			if !p.atEnd() {
				p.next()
			}
		case p.isCmd("tuplet"):
			if err := p.parseTuplet(); err != nil {
				return err
			}
		case p.isCmd("grace"):
			// A short ornamental note borrowing time from the next main
			// note. This subset only models the "\grace X( \glissando
			// Y)" shape — a slide into the main note (real example:
			// Mercy/whiskey-beer-and-wine/intro.ly's own "\grace f16(
			// \glissando g8)") — not grace notes in general (an
			// unslurred/non-glissando grace note has no real guitar-
			// technique meaning this subset renders, and never appears
			// in this app's own corpus): its own duration is irrelevant
			// (borrowed time), only its PITCH survives, remembered until
			// the main note it slides into is parsed.
			p.next() // \grace
			if !p.atEnd() {
				raw := p.next().text
				pitchSpec, _, slurStart, _, _ := splitNoteToken(raw)
				if pitchSpec != "" && pitchSpec != "r" && pitchSpec != "R" {
					if midi, err := p.resolve(pitchSpec, p.lastPitch); err == nil {
						// \relative tracking follows written sequence
						// regardless of grace/real status — real
						// LilyPond resolves the NEXT pitch relative to
						// this grace note, not the last real note.
						p.lastPitch = midi
						if slurStart {
							p.pendingSlideMIDI = midi
						}
					}
				}
			}
			if p.isCmd("glissando") {
				p.next()
			}
		case p.peek().kind == tLAngle:
			if err := p.chordEvent(); err != nil {
				return err
			}
		case p.peek().kind == tWord:
			if err := p.singleEvent(p.next().text); err != nil {
				return err
			}
		default:
			p.next()
		}
	}
	return nil
}

func (p *relParser) parseTuplet() error {
	p.next() // \tuplet
	ratio := p.next().text
	num, den := 1.0, 1.0
	if parts := strings.SplitN(ratio, "/", 2); len(parts) == 2 {
		if n, err := strconv.ParseFloat(parts[0], 64); err == nil {
			num = n
		}
		if d, err := strconv.ParseFloat(parts[1], 64); err == nil {
			den = d
		}
	}
	if p.peek().kind != tLBrace {
		return fmt.Errorf("score: expected '{' after \\tuplet ratio")
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
				goto bodyDone
			}
		}
		p.next()
	}
	return fmt.Errorf("score: unterminated \\tuplet block")
bodyDone:
	body := p.toks[start:p.pos]
	p.next() // consume closing '}'

	sub := &relParser{toks: body, lastPitch: p.lastPitch, minFret: p.minFret, lastDur: p.lastDur, tuning: p.tuning, absolute: p.absolute, pendingSlideMIDI: -1}
	if err := sub.sequence(); err != nil {
		return err
	}
	sub.flushBar()
	// e.g. 3 notes "in the time of 2" each take 2/3 of their written
	// duration — Beat.Beats applies the ratio recorded here.
	for _, bar := range sub.bars {
		for _, beat := range bar.Beats {
			if _, err := durationBeats(beat.Duration); err != nil {
				return err
			}
			beat.TupletNum, beat.TupletDen = int(num), int(den)
			p.addBeat(beat, beat.Beats())
		}
	}
	p.lastPitch, p.lastDur = sub.lastPitch, sub.lastDur
	return nil
}

func (p *relParser) chordEvent() error {
	p.next() // consume '<'
	var notes []Note
	chordRef := p.lastPitch
	firstResolved := p.lastPitch
	first := true
	for p.peek().kind != tRAngle {
		if p.atEnd() {
			return fmt.Errorf("score: unterminated chord '<...>'")
		}
		raw := p.next().text
		pitchSpec, _, _, _, _ := splitNoteToken(raw)
		pitch, err := p.resolve(pitchSpec, chordRef)
		if err != nil {
			return err
		}
		notes = append(notes, Note{MIDI: pitch})
		chordRef = pitch
		if first {
			firstResolved = pitch
			first = false
		}
	}
	p.next() // consume '>'
	// A duration token is OPTIONAL here: "<bes bes'>" with no digits
	// right after inherits the previous event's duration (LilyPond's own
	// rule), which in practice means the very next token is often the
	// start of the FOLLOWING chord/note, not a duration at all.
	durSpec := ""
	if p.peek().kind == tWord && startsWithDigit(p.peek().text) {
		durSpec = p.next().text
	}
	dur, beats, err := parseDuration(durSpec, &p.lastDur)
	if err != nil {
		return err
	}
	if err := assignFrets(notes, p.tuning, p.minFret); err != nil {
		return err
	}
	p.addBeat(Beat{Notes: notes, Duration: dur}, beats)
	p.lastPitch = firstResolved // LilyPond's own quirk: the NEXT event is relative to the chord's FIRST note, not its last
	return nil
}

func (p *relParser) singleEvent(raw string) error {
	pitchSpec, durSpec, slurStart, _, hasArticulation := splitNoteToken(raw)
	if pitchSpec == "r" || pitchSpec == "R" {
		dur, beats, err := parseDuration(durSpec, &p.lastDur)
		if err != nil {
			return err
		}
		p.addBeat(Beat{Rest: true, Duration: dur}, beats)
		return nil
	}
	pitch, err := p.resolve(pitchSpec, p.lastPitch)
	if err != nil {
		return err
	}
	dur, beats, err := parseDuration(durSpec, &p.lastDur)
	if err != nil {
		return err
	}
	notes := []Note{{MIDI: pitch}}
	if err := assignFrets(notes, p.tuning, p.minFret); err != nil {
		return err
	}
	beat := Beat{Notes: notes, Duration: dur, HammerPull: slurStart}
	if hasArticulation {
		if p.peek().kind != tString {
			return fmt.Errorf("score: expected a quoted articulation label after '^', got %q", p.peek().text)
		}
		label := p.next().text
		bend, release, palmMute, err := parseArticulation(label)
		if err != nil {
			return err
		}
		beat.BendSemitones, beat.BendRelease, beat.PalmMute = bend, release, palmMute
	}
	if p.pendingSlideMIDI >= 0 && len(notes) == 1 {
		s := notes[0].String
		if s >= 1 && s <= len(p.tuning) {
			beat.HasSlideFrom = true
			beat.SlideFromFret = p.pendingSlideMIDI - p.tuning[s-1]
		}
	}
	p.pendingSlideMIDI = -1
	p.addBeat(beat, beats)
	p.lastPitch = pitch
	return nil
}

func (p *relParser) addBeat(b Beat, beats float64) {
	p.curBar = append(p.curBar, b)
	p.curBeats += beats
	if p.curBeats >= barBeatsDefault-1e-9 {
		p.bars = append(p.bars, Bar{Beats: p.curBar})
		p.curBar = nil
		p.curBeats -= barBeatsDefault
		if p.curBeats < 1e-9 {
			p.curBeats = 0
		}
	}
}

func (p *relParser) flushBar() {
	if len(p.curBar) > 0 {
		p.bars = append(p.bars, Bar{Beats: p.curBar})
		p.curBar = nil
	}
}

// --- pitch/duration token parsing --------------------------------------

// splitNoteToken strips slur-parenthesis and tie ("~") decoration off a
// raw token, then splits what's left into its pitch spec
// (letter+accidentals+octave marks, or "r"/"R" for a rest) and whatever
// duration-ish suffix follows (digits + optional dot).
//
// slurStart/slurEnd report whether this token carried a slur paren — a
// real LilyPond "c( d e f)" slur always attaches "(" as a SUFFIX of the
// note that opens it and ")" as a suffix of the note that closes it
// (never a prefix; confirmed against every real slur in this app's own
// corpus, e.g. Mercy/always-free/verse.ly's "g,( a,) a, a, a,( g,)" and
// Mercy/bad-spell/intro.ly's "bes16( c'4)"). On a guitar TabStaff this is
// how a hammer-on/pull-off is written — see Beat.HammerPull/PullOff's
// own doc comment for what this becomes downstream.
func splitNoteToken(raw string) (pitchSpec, durSpec string, slurStart, slurEnd, hasArticulation bool) {
	// A text articulation ("^", real LilyPond syntax for "a string
	// markup follows, as a separate token" — see ParseScore's grammar
	// doc comment on the bend/palm-mute vocabulary this carries) is the
	// outermost suffix: it comes after a slur close, e.g. "c4)^".
	hasArticulation = strings.HasSuffix(raw, "^")
	raw = strings.TrimSuffix(raw, "^")
	slurStart = strings.HasSuffix(raw, "(")
	slurEnd = strings.HasSuffix(raw, ")")
	raw = strings.Trim(raw, "()")
	raw = strings.TrimSuffix(raw, "~")
	if raw == "" {
		return "", "", slurStart, slurEnd, hasArticulation
	}
	if raw[0] == 'r' || raw[0] == 'R' {
		return raw[:1], raw[1:], slurStart, slurEnd, hasArticulation
	}
	if raw[0] < 'a' || raw[0] > 'g' {
		return "", raw, slurStart, slurEnd, hasArticulation
	}
	i := 1
	for strings.HasPrefix(raw[i:], "is") || strings.HasPrefix(raw[i:], "es") {
		i += 2
	}
	for i < len(raw) && (raw[i] == '\'' || raw[i] == ',') {
		i++
	}
	return raw[:i], raw[i:], slurStart, slurEnd, hasArticulation
}

// pitchClassOf returns a-g's semitone offset within an octave (c=0).
func pitchClassOf(letter byte) int {
	switch letter {
	case 'c':
		return 0
	case 'd':
		return 2
	case 'e':
		return 4
	case 'f':
		return 5
	case 'g':
		return 7
	case 'a':
		return 9
	case 'b':
		return 11
	}
	return 0
}

// resolve dispatches to relative- or absolute-pitch resolution depending
// on whether this \new TabStaff body had a \relative wrapper — see
// parseTabStaffBody.
func (p *relParser) resolve(spec string, ref int) (int, error) {
	if p.absolute {
		return resolvePitchAbsolute(spec)
	}
	return resolvePitch(spec, ref)
}

// parsePitchSpec splits spec (letter + is/es accidentals + '/,' octave
// marks) into a 0-11 pitch class and an octave-mark count.
func parsePitchSpec(spec string) (pc, marks int, err error) {
	if spec == "" {
		return 0, 0, fmt.Errorf("score: empty pitch")
	}
	letter := spec[0]
	if letter < 'a' || letter > 'g' {
		return 0, 0, fmt.Errorf("score: unrecognized pitch %q", spec)
	}
	i := 1
	acc := 0
	for strings.HasPrefix(spec[i:], "is") || strings.HasPrefix(spec[i:], "es") {
		if spec[i:i+2] == "is" {
			acc++
		} else {
			acc--
		}
		i += 2
	}
	for i < len(spec) {
		switch spec[i] {
		case '\'':
			marks++
		case ',':
			marks--
		}
		i++
	}
	return ((pitchClassOf(letter) + acc) % 12 + 12) % 12, marks, nil
}

// resolvePitch applies LilyPond's relative-octave rule: the written
// pitch class is placed in whichever octave puts it within a perfect
// fourth of ref (the preceding pitch — or, within a chord, the
// preceding note of that same chord), then each '/,' octave mark shifts
// the result by a further whole octave.
func resolvePitch(spec string, ref int) (int, error) {
	pc, marks, err := parsePitchSpec(spec)
	if err != nil {
		return 0, err
	}
	diff := ((pc-ref)%12+18)%12 - 6 // nearest pitch of class pc to ref, within [-6,+5] semitones
	return ref + diff + marks*12, nil
}

// resolvePitchAbsolute applies real LilyPond's absolute (non-\relative)
// pitch convention: an unmarked letter is in octave 3 (unmarked "c" =
// MIDI 48 = C3; "c'" = middle C = MIDI 60).
func resolvePitchAbsolute(spec string) (int, error) {
	pc, marks, err := parsePitchSpec(spec)
	if err != nil {
		return 0, err
	}
	return 48 + pc + marks*12, nil
}

func startsWithDigit(s string) bool {
	return s != "" && s[0] >= '0' && s[0] <= '9'
}

// parseDuration reads a duration suffix ("", "8", "4.", "16"...),
// falling back to lastDur (LilyPond's own "a duration persists until
// respelled" rule) when spec is empty, and returns the LilyPond token to
// store plus its length in quarter-note beats.
func parseDuration(spec string, lastDur *string) (tok string, beats float64, err error) {
	dur := spec
	if dur == "" {
		dur = *lastDur
	} else {
		*lastDur = dur
	}
	b, err := durationBeats(dur)
	if err != nil {
		return "", 0, err
	}
	return dur, b, nil
}

// DurationBeats converts a LilyPond duration token to quarter-note beats
// — exported for renderers that need proportional (not uniform) spacing
// between beats, the real engraving convention: a half note takes twice
// the horizontal room of a quarter, matching real LilyPond output
// (reviewed against LilyPond's own spacing model — space between two
// notes is a function of the shorter duration's "spacing increment,"
// which a simple proportional-to-duration width approximates well for a
// single-voice riff with no cross-voice alignment to reconcile).
func DurationBeats(dur string) (float64, error) { return durationBeats(dur) }

func durationBeats(dur string) (float64, error) {
	dotted := strings.HasSuffix(dur, ".")
	base := strings.TrimRight(dur, ".")
	n, err := strconv.Atoi(base)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("score: invalid duration %q", dur)
	}
	beats := 4.0 / float64(n)
	if dotted {
		beats *= 1.5
	}
	return beats, nil
}

// DurationGlyph reads a Beat.Duration token (e.g. "4", "8", "16.") and
// returns the rhythmic marking real tab notation draws under/through the
// fret numbers (the same convention LilyPond's \tabFullNotation and
// Guitar Pro both use: a plain stem for a quarter or longer, a stem with
// one flag per eighth-note halving, a dot for a dotted duration) — the
// crucial "which beat is this, how long does it last" information a
// bare fret-number grid can't convey on its own.
func DurationGlyph(dur string) (flags int, dotted bool) {
	dotted = strings.HasSuffix(dur, ".")
	base := strings.TrimRight(dur, ".")
	n, err := strconv.Atoi(base)
	if err != nil || n <= 0 {
		return 0, dotted
	}
	// 4 (quarter) or longer (2, 1): 0 flags. 8->1, 16->2, 32->3, 64->4 —
	// each halving of the note value adds one flag, standard notation.
	for v := 8; v <= n; v *= 2 {
		flags++
	}
	return flags, dotted
}

// --- string/fret assignment ---------------------------------------------

// assignFrets picks a string+fret for each of notes (processed together
// so a chord doesn't reuse the same string twice), preferring the
// lowest fret at or above minFret — matching LilyPond's own documented
// minimumFret rule ("uses fret numbers >= this; if none exists for a
// given note, the lowest possible fret is used regardless") — applied
// per note rather than as a true whole-chord optimization. This is a
// heuristic, not a reimplementation of LilyPond's real fretting
// algorithm, but it reproduces the real Mercy/mercy.ly riff's own
// engraved fret numbers exactly where spot-checked against the legacy
// book's printed PDF.
// assignFrets picks a string+fret for each of notes, reproducing real
// LilyPond's own algorithm (scm/translation-functions.scm,
// determine-frets-and-strings), not an approximation of it:
//   - notes are assigned in DESCENDING pitch order (highest first), so
//     a chord's top note gets first pick of the thinnest strings —
//     real LilyPond sorts its "no explicit string" pass the same way.
//   - for each note, strings are tried in string-NUMBER order (1, 2,
//     3, ... — thinnest/highest string first) and the FIRST one whose
//     fret is both unused and >= minFret wins. This is a strict
//     "prefer the highest string that's legal" rule, not a
//     distance-from-minFret optimization — LilyPond's `find` over
//     `free-strings` (built as `(iota N 1)`, i.e. string 1 first)
//     stops at the first qualifying string, it never compares
//     candidates against each other.
//   - fret 0 (open string) only qualifies when minFret <= 0: every
//     real riff this subset has seen sets \set TabStaff.
//     restrainOpenStrings = ##t, which in real LilyPond means open
//     strings must ALSO clear minimumFret like any other fret — so
//     that's this package's only behavior too (there is no non-
//     restrained mode to opt into from the LilyPond-subset source).
func assignFrets(notes []Note, tuning []int, minFret int) error {
	order := make([]int, len(notes))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return notes[order[a]].MIDI > notes[order[b]].MIDI })

	used := make(map[int]bool, len(notes))
	for _, i := range order {
		placed := false
		for s, open := range tuning {
			if used[s] {
				continue
			}
			fret := notes[i].MIDI - open
			if fret < 0 || fret > 24 || fret < minFret {
				continue
			}
			notes[i].String = s + 1
			notes[i].Fret = fret
			used[s] = true
			placed = true
			break
		}
		// A note that fits no open string (too low for any string, or
		// every string already claimed within this chord cluster, or
		// below \set TabStaff.minimumFret) used to fall through silently
		// as the zero Note{String:0, Fret:0} — a value indistinguishable
		// from a real string-1-fret-0 note, caught only much later as a
		// cryptic alphaTab renderer crash ("Note string is out of range")
		// once it reached ToAlphaTex. Surfacing it here instead matches
		// handlePutSongScore's own "catch it now, not on every later
		// read" save-time validation.
		if !placed {
			return fmt.Errorf("score: %s doesn't fit this tuning at minimumFret %d (too low, or every string already used by this chord)", midiToAlphaTexNote(notes[i].MIDI), minFret)
		}
	}
	return nil
}
