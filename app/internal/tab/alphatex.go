package tab

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// noteNames spells a MIDI pitch class the way alphaTeX's \tuning directive
// expects ("E2", "F#2", ...) — alphaTeX always spells accidentals sharp,
// never flat, regardless of how the source LilyPond pitch was written
// (this package's own MIDI numbers have already thrown that spelling
// away, same as a real tuning reference always does).
var noteNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

func midiToAlphaTexNote(midi int) string {
	pc := ((midi % 12) + 12) % 12
	octave := midi/12 - 1 // MIDI 60 = C4, matching this package's own MIDI convention
	return noteNames[pc] + strconv.Itoa(octave)
}

// ToAlphaTex renders riff as alphaTeX source — real engraved tab/standard
// notation via the vendored alphaTab JS renderer (static/js/vendor/
// alphatab), replacing this app's own hand-rolled CSS/SVG tab drawing for
// any row that has full riff coverage (see attachTabRiffs/viewerLine.TabOnly,
// internal/httpapi/handlers_pages.go).
//
// \tuning lists every string open pitch, high to low — same order as
// Riff.Tuning itself (index 0 = string 1 = highest), so no remapping is
// needed between this package's own string-numbering convention and
// alphaTab's. clef, when non-empty ("F4" for a 4-string bass, the only
// case this app currently calls with one — see ToAlphaTexBass), is
// emitted as a per-bar \clef directive on the first bar only (matching
// real engraving: the clef is stated once, not repeated every bar) —
// needed because alphaTab infers treble by default even for a bass
// tuning when only the standard-notation staff (not Tab) is shown; see
// this function's own exploration notes in the session this was written
// in for why that inference doesn't happen automatically.
//
// chordLabels, when non-nil, is one chord-symbol string per bar (""
// for a bar with no label) attached to that bar's FIRST beat via
// alphaTeX's own "{ch "Name"}" beat property — alphaTab then draws it
// itself, positioned by its own layout engine, which is why this is
// done here rather than overlaying this app's own chord-grid markup on
// top of an alphaTab canvas (the two would drift out of alignment: this
// app's chord-grid beat slots and a riff's own note-level beats are
// different timelines — see attachTabRiffs). Only the chord actually
// STARTING a bar is labeled (a real limitation when a bar's grid: has a
// genuine mid-bar chord change — not yet placed on the riff's own
// matching beat; flagged, not silently dropped — see this function's
// caller).
func ToAlphaTex(riff *Riff, clef string, chordLabels []string) string {
	var b strings.Builder
	b.WriteString(`\tuning (`)
	for i, midi := range riff.Tuning {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(midiToAlphaTexNote(midi))
	}
	b.WriteString(")\n.\n")

	for bi, bar := range riff.Bars {
		if bi > 0 {
			b.WriteString(" | ")
		}
		if bi == 0 && clef != "" {
			b.WriteString(`\clef `)
			b.WriteString(clef)
			b.WriteByte(' ')
		}
		var label string
		if bi < len(chordLabels) {
			label = chordLabels[bi]
		}
		for i, beat := range bar.Beats {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeBeat(&b, beat, label)
			label = ""
		}
	}
	b.WriteString(" |")
	return b.String()
}

// ToAlphaTexBass is ToAlphaTex with the one per-riff hint this app's own
// parser doesn't otherwise carry: a 4-string riff is always a bass part,
// and alphaTab needs an explicit \clef to show it in F clef rather than
// its treble default (see ToAlphaTex's doc comment) — 6+ strings get no
// clef directive at all, matching every guitar riff tested this session,
// which alphaTab already clefs correctly on its own.
func ToAlphaTexBass(riff *Riff, chordLabels []string) string {
	clef := ""
	if len(riff.Tuning) <= 4 {
		clef = "F4"
	}
	return ToAlphaTex(riff, clef, chordLabels)
}

// MixerTrack describes one \track block of a ToAlphaTexMulti document, in
// declaration order — alphaTab assigns Score.tracks indexes in that same
// order, so a slice index here IS the client's track index.
type MixerTrack struct {
	Name    string
	Drums   bool
	Program int // General MIDI program (0-based); -1 for percussion
}

// gmProgramFor guesses a General MIDI program from a track's own name —
// a riff stores tuning and frets, not the instrument it was written for,
// and without this every track plays as GM program 0 (piano).
func gmProgramFor(name string, numStrings int) int {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "bass") || numStrings <= 4:
		if strings.Contains(n, "fretless") {
			return 35
		}
		if strings.Contains(n, "pick") {
			return 34
		}
		return 33
	case strings.Contains(n, "nylon") || strings.Contains(n, "classical"):
		return 24
	case strings.Contains(n, "acoustic") || strings.Contains(n, "steel"):
		return 25
	case strings.Contains(n, "jazz"):
		return 26
	case strings.Contains(n, "dist") || strings.Contains(n, "lead") || strings.Contains(n, "metal"):
		return 30
	case strings.Contains(n, "overdrive") || strings.Contains(n, "crunch") || strings.Contains(n, "rock"):
		return 29
	case strings.Contains(n, "piano") || strings.Contains(n, "keys"):
		return 0
	case strings.Contains(n, "organ"):
		return 16
	}
	return 27 // clean electric guitar
}

// ToAlphaTexMulti renders several tracks into ONE alphaTeX document so
// alphaTab's player can play them on a single clock (the Mixer view).
// Every track must have the same bar count. No multiBarRest anywhere:
// the Mixer gives every bar its own fixed width to line notation up with
// the reference recording, so bars must never be merged.
func ToAlphaTexMulti(tracks []NamedRiff, tempo, timeNum, timeDen int) (tex string, meta []MixerTrack) {
	if tempo <= 0 {
		tempo = 120
	}
	ts := ""
	if timeNum > 0 && timeDen > 0 && !(timeNum == 4 && timeDen == 4) {
		ts = fmt.Sprintf(`\ts %d %d `, timeNum, timeDen)
	}
	var b strings.Builder
	b.WriteString(`\tempo `)
	b.WriteString(strconv.Itoa(tempo))
	b.WriteString("\n")
	for _, t := range tracks {
		name := t.Name
		if name == "" {
			name = "Track"
		}
		if t.Riff.Drums {
			writeDrumTrackBlock(&b, name, t.Riff, ts)
			meta = append(meta, MixerTrack{Name: name, Drums: true, Program: -1})
			continue
		}
		prog := gmProgramFor(name, len(t.Riff.Tuning))
		writeMelodicTrackBlock(&b, name, prog, t.Riff, ts)
		meta = append(meta, MixerTrack{Name: name, Program: prog})
	}
	return b.String(), meta
}

func writeMelodicTrackBlock(b *strings.Builder, name string, program int, riff *Riff, ts string) {
	fmt.Fprintf(b, "\\track \"%s\" { instrument %d }\n\\staff {tabs}\n\\tuning (", escapeAlphaTexString(name), program)
	for i, midi := range riff.Tuning {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(midiToAlphaTexNote(midi))
	}
	b.WriteString(")\n.\n")
	for bi, bar := range riff.Bars {
		if bi > 0 {
			b.WriteString(" | ")
		} else {
			b.WriteString(ts)
		}
		for i, beat := range bar.Beats {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeBeat(b, beat, "")
		}
	}
	b.WriteString(" |\n")
}

func writeDrumTrackBlock(b *strings.Builder, name string, riff *Riff, ts string) {
	fmt.Fprintf(b, "\\track \"%s\" { instrument percussion }\n\\staff {score}\n", escapeAlphaTexString(name))
	used := map[int]bool{}
	var order []int
	for _, bar := range riff.Bars {
		for _, beat := range bar.Beats {
			for _, n := range beat.Notes {
				if !used[n.MIDI] {
					used[n.MIDI] = true
					order = append(order, n.MIDI)
				}
			}
		}
	}
	sort.Ints(order)
	for _, midi := range order {
		if a := drumArticulationName[midi]; a != "" {
			fmt.Fprintf(b, "\\articulation (%s %d)\n", a, midi)
		}
	}
	b.WriteString(".\n")
	for bi, bar := range riff.Bars {
		if bi > 0 {
			b.WriteString(" | ")
		} else {
			b.WriteString(ts)
			b.WriteString(`\clef neutral `)
		}
		for i, beat := range bar.Beats {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeDrumBeat(b, beat, "")
		}
	}
	b.WriteString(" |\n")
}

func writeBeat(b *strings.Builder, beat Beat, chordLabel string) {
	dur, dotted := splitDuration(beat.Duration)
	switch {
	case beat.Rest:
		b.WriteString("r.")
		b.WriteString(dur)
	case len(beat.Notes) == 1:
		n := beat.Notes[0]
		// Beat.HasSlideFrom (a "\grace X( \glissando Y)" in the source —
		// see its own doc comment) becomes a real alphaTeX grace note: a
		// synthetic beat at SlideFromFret, marked both as a note-level
		// slide-out ("{sl}", needs the same chord-paren wrapping "{h}"
		// does — verified empirically) and a beat-level grace ("{gr}",
		// LilyPond's own "borrowed time" with no duration of its own —
		// the shortest real alphaTeX duration, 32nd, stands in for that;
		// alphaTab's "{gr}" already excludes it from the bar's own
		// written duration total, so this doesn't throw off the bar's
		// overall length the way it would if "{gr}" didn't exist).
		if beat.HasSlideFrom {
			b.WriteString("(")
			b.WriteString(strconv.Itoa(beat.SlideFromFret))
			b.WriteByte('.')
			b.WriteString(strconv.Itoa(n.String))
			b.WriteString(`{sl}`)
			b.WriteString(").32{gr} ")
		}
		// alphaTeX note-level properties ("{h}" hammer-on/pull-off,
		// "{b N}" bend by N quarter-tones, "{pm}" palm mute) — verified
		// empirically against the vendored library, not documented
		// anywhere obvious — only parse when the note is wrapped in
		// chord parens, even for a single note: "(3.6{h}).8", never
		// "3.6{h}.8". alphaTab itself works out hammer-on vs. pull-off
		// (and draws "H" or "P") from comparing this note's pitch to the
		// next, so Beat.HammerPull only needs to mark the origin, not
		// which direction it is.
		var noteProps []string
		if beat.HammerPull {
			noteProps = append(noteProps, "h")
		}
		if beat.BendSemitones > 0 {
			// alphaTeX's "{b ...}" effect takes a BEND POINT LIST, not a
			// bare amount — confirmed empirically this session: "{b 4}"
			// parses without error but silently produces NO bend glyph
			// at all (every bend in a batch of GP-imported songs was
			// invisible because of exactly this), while "{b (0 4)}" (one
			// point: time-position 0, value 4) renders the real curved
			// arrow + "full"/fractional label. Bend amounts are in
			// quarter-tones; this package's own BendSemitones is in
			// semitones (see its own doc comment), so *2. BendRelease
			// isn't separately representable here (a proper 3-point
			// release curve was tried and produced spurious extra
			// fret-number glyphs — not worth chasing for a secondary
			// nuance); a plain bend-up-and-hold is the common case and
			// what every hand-written test fixture uses.
			noteProps = append(noteProps, fmt.Sprintf("b (0 %d)", int(beat.BendSemitones*2)))
		}
		if beat.PalmMute {
			noteProps = append(noteProps, "pm")
		}
		if n.Tied {
			// alphaTeX's "{t}" note effect — marks this note as tied to
			// the same-pitch note immediately before it, so alphaTab
			// draws a tie curve and suppresses the re-attack rather than
			// rendering a second freshly-picked note. GP import already
			// extracts this correctly (gp-import.js reads alphaTab's own
			// note.isTieDestination into Note.Tied), but writeBeat never
			// read it back out until now — every tied note was silently
			// rendered as an ordinary struck note.
			noteProps = append(noteProps, "t")
		}
		if len(noteProps) > 0 {
			b.WriteByte('(')
			b.WriteString(strconv.Itoa(n.Fret))
			b.WriteByte('.')
			b.WriteString(strconv.Itoa(n.String))
			b.WriteByte('{')
			b.WriteString(strings.Join(noteProps, " "))
			b.WriteByte('}')
			b.WriteString(").")
			b.WriteString(dur)
		} else {
			b.WriteString(strconv.Itoa(n.Fret))
			b.WriteByte('.')
			b.WriteString(strconv.Itoa(n.String))
			b.WriteByte('.')
			b.WriteString(dur)
		}
	default:
		b.WriteByte('(')
		for i, n := range beat.Notes {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(strconv.Itoa(n.Fret))
			b.WriteByte('.')
			b.WriteString(strconv.Itoa(n.String))
			if n.Tied {
				b.WriteString("{t}")
			}
		}
		b.WriteString(").")
		b.WriteString(dur)
	}
	if dotted || chordLabel != "" {
		b.WriteByte('{')
		wrote := false
		if dotted {
			b.WriteString("d")
			wrote = true
		}
		if chordLabel != "" {
			if wrote {
				b.WriteByte(' ')
			}
			b.WriteString(`ch "`)
			b.WriteString(escapeAlphaTexString(chordLabel))
			b.WriteByte('"')
		}
		b.WriteByte('}')
	}
}

// escapeAlphaTexString escapes a chord symbol for alphaTeX's own quoted
// string-literal syntax — only '"' and '\' need it; chord symbols never
// contain either in this app's grammar (chordsym), but a hand-typed grid
// token technically could.
func escapeAlphaTexString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// drumArticulationName maps a General MIDI percussion note number (see
// drumPitchNames, drums.go) to the fixed alphaTeX articulation name
// ToAlphaTexDrums declares for it. These are this package's OWN chosen
// names, each declared via a "\articulation (Name N)" line in the output
// (see ToAlphaTexDrums) rather than relied on from alphaTab's built-in
// default articulation table — that table's exact names/casing (e.g.
// whether it's "KickHit" or "Kickhit") isn't reliably documented, so
// self-declaring sidesteps the ambiguity entirely: every name this
// function emits, it also defines.
var drumArticulationName = map[int]string{
	36: "Kick",
	37: "SideStick",
	38: "Snare",
	41: "TomFloorLow",
	42: "HiHatClosed",
	43: "TomFloorHigh",
	44: "HiHatPedal",
	45: "TomLow",
	46: "HiHatOpen",
	47: "TomMid",
	49: "Crash",
	50: "TomHigh",
	51: "Ride",
	53: "RideBell",
	56: "Cowbell",
}

// ToAlphaTexDrums renders a drum Riff (Riff.Drums == true) as alphaTeX
// source for the vendored alphaTab renderer, the drum-notation
// counterpart of ToAlphaTex/ToAlphaTexBass. A percussion track needs two
// things neither of those emit: "instrument percussion" on the track
// (alphaTab then draws a standard notation staff with GM percussion
// noteheads instead of a guitar tab staff — see alphatab-tab.js, which
// detects this same literal string to switch its own staveProfile), and
// an explicit "\articulation (Name N)" line per distinct drum piece this
// riff actually uses (only those, not the whole GM kit) — see
// drumArticulationName's own doc comment for why these are self-declared
// rather than assumed from alphaTab's built-in defaults. "multiBarRest"
// (confirmed against the vendored library — a bare track-level flag,
// score.stylesheet.perTrackMultiBarRest) consolidates a run of
// consecutive full-bar rests into one real engraved multi-measure-rest
// glyph with its own bar count, instead of drawing each empty bar out —
// the same convention this chart's own .multi-rest-row already uses
// elsewhere, now also covering genuine rest bars WITHIN a written drum
// part (a bridge where the kit lays out, say), not just a section with
// no drum notation of its own at all.
//
// chordLabels, when non-nil, is one chord-symbol string per bar ("" for
// a bar with no label) — same convention and same beat-property
// ("{ch "Name"}") ToAlphaTex already uses for a melodic riff, so a
// drummer reading real engraved bars (not the no-notation default
// multi-rest case) still sees the chord changes they're drumming to.
//
// boundaries, when non-nil, marks which bars start a DIFFERENT section
// than the one before it (see buildDrumMap, handlers_pages.go, which
// builds ONE alphaTab mount per LINE, always — several sections' worth
// of bars share a line, flowing straight from one into the next, with
// NO per-section mount boundary: that's what keeps the clef and time
// signature drawn exactly once, at the very start of the line, instead
// of once per section like two earlier versions of this did). Without a
// boundary marked, two adjacent sections' own all-rest runs silently
// consolidate into ONE combined multiBarRest glyph with no indication of
// where one ends and the other begins — confirmed live and flagged
// directly (Belle of the Ball: a line showing "Pickup"/"Intro" labels
// above a single unbroken "8", no way to tell 2 bars of Pickup from 6 of
// Intro).
//
// A boundary bar gets an alphaTeX "\tempo N " directive right before it
// — tempo, not "\section "" "" ", a thing this function tried first and
// which looked right in isolation (confirmed: an empty-text marker draws
// no visible label) but has a real side effect confirmed live against
// this exact multi-boundary case: the bar the marker sits on can't join
// EITHER neighboring run — "2 | 1 | 5" for an 8-bar line split after bar
// 2, not the clean "2 | 6" a reader needs to actually tell the two
// sections' own counts apart. A tempo marking breaks multiBarRest's own
// consolidation the same way (confirmed against the vendored source:
// multiBarRest's break condition checks a bar-level "tempo changed"
// flag, same mechanism as the section check, just a different trigger)
// WITHOUT that side effect — the marked bar rejoins whichever run
// follows it cleanly. Visually invisible as long as the caller also
// disables NotationElement.EffectTempo (alphatab-tab.js): this value
// never reaches the song's own tempo/metronome, which lives in the
// .song file's "tempo:" field, nowhere near this per-line alphaTeX.
//
// tempo alone, reused unchanged at every boundary, does NOT work: a
// "\tempo" directive whose value matches what's already in effect is a
// silent no-op (confirmed live — two boundaries both declaring the same
// value merge their three runs into two, "2 | 2 | 4" collapsing to
// "2 | 6"), so each successive boundary alternates tempo/tempo+1 to
// guarantee it always genuinely differs from the one right before it —
// confirmed live as reliable across any number of boundaries in one
// line.
func ToAlphaTexDrums(riff *Riff, chordLabels []string, boundaries []bool, tempo int) string {
	var b strings.Builder
	b.WriteString("\\track \"Drums\" { instrument percussion multiBarRest }\n")

	used := map[int]bool{}
	var order []int
	for _, bar := range riff.Bars {
		for _, beat := range bar.Beats {
			for _, n := range beat.Notes {
				if !used[n.MIDI] {
					used[n.MIDI] = true
					order = append(order, n.MIDI)
				}
			}
		}
	}
	sort.Ints(order)
	for _, midi := range order {
		name := drumArticulationName[midi]
		if name == "" {
			continue
		}
		b.WriteString(`\articulation (`)
		b.WriteString(name)
		b.WriteByte(' ')
		b.WriteString(strconv.Itoa(midi))
		b.WriteString(")\n")
	}
	b.WriteString(".\n")

	if tempo <= 0 {
		tempo = 120
	}
	boundaryTempo := tempo + 1 // alternates with tempo at each successive boundary — see this function's own doc comment
	for bi, bar := range riff.Bars {
		if bi > 0 {
			b.WriteString(" | ")
		}
		if bi < len(boundaries) && boundaries[bi] {
			b.WriteString(`\tempo `)
			b.WriteString(strconv.Itoa(boundaryTempo))
			b.WriteByte(' ')
			boundaryTempo = tempo + tempo + 1 - boundaryTempo // swap between tempo and tempo+1
		}
		if bi == 0 {
			b.WriteString(`\clef neutral `)
		}
		var label string
		if bi < len(chordLabels) {
			label = chordLabels[bi]
		}
		for i, beat := range bar.Beats {
			if i > 0 {
				b.WriteByte(' ')
			}
			writeDrumBeat(&b, beat, label)
			label = ""
		}
	}
	b.WriteString(" |")
	return b.String()
}

func writeDrumBeat(b *strings.Builder, beat Beat, chordLabel string) {
	dur, dotted := splitDuration(beat.Duration)
	switch {
	case beat.Rest:
		b.WriteString("r.")
		b.WriteString(dur)
	case len(beat.Notes) == 1:
		b.WriteString(drumArticulationName[beat.Notes[0].MIDI])
		b.WriteByte('.')
		b.WriteString(dur)
	default:
		b.WriteByte('(')
		for i, n := range beat.Notes {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(drumArticulationName[n.MIDI])
		}
		b.WriteString(").")
		b.WriteString(dur)
	}
	if dotted || chordLabel != "" {
		b.WriteByte('{')
		wrote := false
		if dotted {
			b.WriteString("d")
			wrote = true
		}
		if chordLabel != "" {
			if wrote {
				b.WriteByte(' ')
			}
			b.WriteString(`ch "`)
			b.WriteString(escapeAlphaTexString(chordLabel))
			b.WriteByte('"')
		}
		b.WriteByte('}')
	}
}

// splitDuration splits a LilyPond duration token ("4", "4.", "16") into
// alphaTeX's own duration number and a separate dotted flag — alphaTeX
// has no "." duration suffix of its own, only a trailing "{d}" beat
// property (verified empirically against the vendored library: a bare
// trailing "." on the duration is silently accepted but has NO effect
// at all, and is NOT a parse error — a dot written that way would
// silently vanish rather than fail loudly, which is exactly why this
// function exists instead of just forwarding the token as-is).
func splitDuration(d string) (dur string, dotted bool) {
	if strings.HasSuffix(d, ".") {
		return strings.TrimSuffix(d, "."), true
	}
	return d, false
}

// BarBeats is a bar's written length in quarter-note beats (grace notes,
// which take no time of their own, aside).
func BarBeats(bar Bar) float64 {
	total := 0.0
	for _, b := range bar.Beats {
		if d, err := durationBeats(b.Duration); err == nil {
			total += d
		}
	}
	return total
}

// RestBeats is a run of rests lasting exactly beats quarter notes, longest
// values first (down to a 32nd).
func RestBeats(beats float64) []Beat {
	values := []struct {
		dur   string
		beats float64
	}{{"1", 4}, {"2.", 3}, {"2", 2}, {"4.", 1.5}, {"4", 1}, {"8.", 0.75}, {"8", 0.5}, {"16.", 0.375}, {"16", 0.25}, {"32", 0.125}}
	var out []Beat
	for _, v := range values {
		for beats >= v.beats-1e-9 {
			out = append(out, Beat{Rest: true, Duration: v.dur})
			beats -= v.beats
		}
	}
	return out
}
