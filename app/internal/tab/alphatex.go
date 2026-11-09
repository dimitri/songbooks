package tab

import (
	"fmt"
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
			// alphaTeX bend amounts are in quarter-tones; this package's
			// own BendSemitones is in semitones (see its own doc
			// comment), so *2. BendRelease isn't separately representable
			// here (alphaTeX infers bend shape from a point list this
			// package doesn't build — a plain bend-up is the common
			// case and what every hand-written test fixture uses).
			noteProps = append(noteProps, fmt.Sprintf("b %d", int(beat.BendSemitones*2)))
		}
		if beat.PalmMute {
			noteProps = append(noteProps, "pm")
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
