package tab

import (
	"fmt"
	"sort"
	"strings"
)

// drumPitchNames maps LilyPond's own \drummode short names (ly/drumpitch-
// init.ly, the real upstream table) to a General MIDI percussion note
// number — the same map both LilyPond's own \drummode output and GM-
// based tools (alphaTab's built-in percussion articulation table among
// them) are independently derived from, so a drum Riff's Notes carry a
// number every side already agrees on, not an invented one (see
// drumArticulationName in alphatex.go, which goes the other way: GM
// number -> this app's own alphaTeX articulation name). Deliberately
// bounded to what a real rock/pop drum chart needs — see ParseScore's
// own doc comment for this package's "intentionally bounded subset"
// philosophy; LilyPond itself defines quite a few more (congas, bongos,
// claves, ...) a chart like this has never needed.
var drumPitchNames = map[string]int{
	"bd":       36, // bass drum
	"ss":       37, // side stick
	"sn":       38, // snare
	"hh":       42, // hi-hat — LilyPond's own default alias for hhc
	"hhc":      42, // hi-hat, closed
	"hho":      46, // hi-hat, open
	"hhp":      44, // hi-hat, pedal
	"cymc":     49, // crash cymbal
	"cymr":     51, // ride cymbal
	"ridebell": 53,
	"tomfl":    41, // floor tom, low
	"tomfh":    43, // floor tom, high
	"toml":     45, // tom, low
	"tomm":     47, // tom, mid
	"tomh":     50, // tom, high
	"cb":       56, // cowbell
}

// drumNamesByLengthDesc is drumPitchNames' keys, longest first, so a
// greedy prefix match (splitDrumToken) tries "tomfl"/"tomfh" before the
// "toml"/"tomm"/"tomh" it's also a prefix of a substring of, and those
// before the bare 2-letter names — the same "longest token wins" rule a
// real LilyPond lexer applies to its own drum pitch names.
var drumNamesByLengthDesc = func() []string {
	names := make([]string, 0, len(drumPitchNames))
	for n := range drumPitchNames {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j] // stable, deterministic tie-break
	})
	return names
}()

// parseDrumStaffBody parses one "\new DrumStaff { ... }" block's
// contents: skips anything this subset doesn't act on (\tempo, \clef,
// \time at the DrumStaff level — meter is a song-level concern, same as
// parseTabStaffBody's own reasoning) looking for the \drummode { ... }
// block, which is where the actual hits live in every real drum
// notation file this subset targets, same shape as a guitar TabStaff's
// \relative block.
func parseDrumStaffBody(toks []token) (*Riff, error) {
	p := &lyParser{toks: toks}
	for !p.atEnd() {
		if p.isCmd("drummode") {
			p.next()
			body, err := p.skipBalanced()
			if err != nil {
				return nil, err
			}
			return parseDrumModeBody(body)
		}
		if err := p.skipUnknownStatement(); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("score: \\new DrumStaff has no \\drummode block")
}

// parseDrumModeBody parses a \drummode block's contents into a Riff: a
// flat sequence of drum-name+duration tokens ("bd4 sn4", LilyPond's own
// \drummode grammar) or simultaneous-hit chords ("<bd hh>8" — a kick and
// a closed hi-hat on the same beat, written exactly like a real LilyPond
// chord cluster), rests ("r4"), bar lines inferred from cumulative
// duration against an assumed 4/4 (same barBeatsDefault every other riff
// kind in this package uses — see relParser.addBeat). No \relative
// wrapper, no pitch resolution: a drum name IS the note, there's no
// "distance from the previous note" the way a pitched riff has.
func parseDrumModeBody(toks []token) (*Riff, error) {
	p := &lyParser{toks: toks}
	var bars []Bar
	var curBar []Beat
	curBeats := 0.0
	lastDur := "4"

	addBeat := func(b Beat, beats float64) {
		curBar = append(curBar, b)
		curBeats += beats
		if curBeats >= barBeatsDefault-1e-9 {
			bars = append(bars, Bar{Beats: curBar})
			curBar = nil
			curBeats -= barBeatsDefault
			if curBeats < 1e-9 {
				curBeats = 0
			}
		}
	}

	for !p.atEnd() {
		switch {
		case p.isCmd("time"):
			p.next()
			p.skipOneArg() // meter spec, e.g. "4/4" — this subset always groups bars at barBeatsDefault, same simplification every other riff kind makes
		case p.peek().kind == tLAngle:
			p.next()
			var notes []Note
			for p.peek().kind != tRAngle {
				if p.atEnd() {
					return nil, fmt.Errorf("score: unterminated drum chord '<...>'")
				}
				raw := p.next().text
				name, _, isRest := splitDrumToken(raw)
				if isRest || name == "" {
					return nil, fmt.Errorf("score: unrecognized drum name %q in chord", raw)
				}
				notes = append(notes, Note{MIDI: drumPitchNames[name]})
			}
			p.next() // consume '>'
			durSpec := ""
			if p.peek().kind == tWord && startsWithDigit(p.peek().text) {
				durSpec = p.next().text
			}
			dur, beats, err := parseDuration(durSpec, &lastDur)
			if err != nil {
				return nil, err
			}
			addBeat(Beat{Notes: notes, Duration: dur}, beats)
		case p.peek().kind == tWord:
			raw := p.next().text
			name, durSpec, isRest := splitDrumToken(raw)
			if isRest {
				dur, beats, err := parseDuration(durSpec, &lastDur)
				if err != nil {
					return nil, err
				}
				addBeat(Beat{Rest: true, Duration: dur}, beats)
				continue
			}
			if name == "" {
				return nil, fmt.Errorf("score: unrecognized drum token %q", raw)
			}
			dur, beats, err := parseDuration(durSpec, &lastDur)
			if err != nil {
				return nil, err
			}
			addBeat(Beat{Notes: []Note{{MIDI: drumPitchNames[name]}}, Duration: dur}, beats)
		default:
			return nil, fmt.Errorf("score: unexpected token %q in \\drummode", p.peek().text)
		}
	}
	if len(curBar) > 0 {
		bars = append(bars, Bar{Beats: curBar})
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("score: \\drummode has no note content")
	}
	return &Riff{Drums: true, Bars: bars}, nil
}

// splitDrumToken splits a raw \drummode token ("bd4", "hh8.", "r4", bare
// "cymc" inheriting the previous duration) into its drum-name part and
// whatever duration suffix follows, matching splitNoteToken's job for
// pitched tokens but against the fixed drumPitchNames vocabulary instead
// of a letter-pitch grammar (drum names don't all start with a-g, so
// that function's own pitch-letter check doesn't apply here).
func splitDrumToken(raw string) (name, durSpec string, isRest bool) {
	raw = strings.TrimSuffix(raw, "~")
	if raw == "" {
		return "", "", false
	}
	if raw[0] == 'r' || raw[0] == 'R' {
		return "", raw[1:], true
	}
	for _, n := range drumNamesByLengthDesc {
		if strings.HasPrefix(raw, n) {
			return n, raw[len(n):], false
		}
	}
	return "", raw, false
}
