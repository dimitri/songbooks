package tab

import (
	"strconv"
	"strings"

	"app/internal/song"
)

// StartsWithPickup reports whether sg.Sections[si] — the song's first
// section with a grid — opens with a pickup bar: its score writes a first
// bar shorter than the time signature (an anacrusis, as a Guitar Pro
// import keeps it), or its grid's first bar carries no chord at all ("?",
// "." or "R": a count-in bar). The web chart (httpapi) and the PDF
// (pdfnative) both ask here, so that both put it on the same row.
func StartsWithPickup(sg *song.Song, si int) bool {
	if si < 0 || si >= len(sg.Sections) {
		return false
	}
	sec := sg.Sections[si]
	if len(sec.Measures) == 0 {
		return false
	}
	// A "lead:" line (song.WithLeads) says so outright.
	if sec.Measures[0].Lead || sec.Lead > 0 {
		return true
	}
	timeSig := sec.Time
	if timeSig == "" {
		timeSig = sg.Get("time")
	}
	if timeSig == "" {
		timeSig = "4/4"
	}
	num, den, _ := strings.Cut(timeSig, "/")
	full := float64(atoiOr(num, 4)) * 4 / float64(atoiOr(den, 4))
	// The score decides when there is one. A track playing from the first
	// beat of a full bar makes it bar 1; one whose notes only come in late
	// (a first bar shorter than a full one, or opening with rests over at
	// least half of it — an anacrusis padded to a full bar) makes it a
	// pickup, as does a first bar silent in every track under a silent
	// grid bar (a count-in).
	scored, late, fromStart := false, false, false
	for _, sv := range sec.Scores {
		riff, err := ParseVoice(sv.Label, sv.Body)
		if err != nil || len(riff.Bars) == 0 {
			continue
		}
		scored = true
		bar := riff.Bars[0]
		lead, notes := 0.0, false
		for _, b := range bar.Beats {
			if !b.Rest {
				notes = true
				break
			}
			lead += b.Beats()
		}
		if !notes {
			continue
		}
		if BarBeats(bar) < full-1e-9 || lead >= full/2-1e-9 {
			late = true
		} else {
			fromStart = true
		}
	}
	if scored {
		// A first bar silent in every track is a count-in only under a
		// silent grid bar too: silence under a chord is an intro bar.
		return !fromStart && (late || SilentBar(sec.Measures[0].Tokens))
	}
	// No score: a silent first grid bar ("?", "R", N.C.) — but only in a
	// grid that names chords somewhere; one that names none ("R" in every
	// bar, e.g. a Guitar Pro import without chord diagrams) says nothing.
	return SilentBar(sec.Measures[0].Tokens) && gridNamesChords(sg)
}

// FirstGridSection is the index of the song's first section with a grid
// (the one a pickup can open), -1 when none has one.
func FirstGridSection(sg *song.Song) int {
	for i, sec := range sg.Sections {
		if len(sec.Measures) > 0 {
			return i
		}
	}
	return -1
}

// gridNamesChords: some bar of the song's chord grid names a chord.
func gridNamesChords(sg *song.Song) bool {
	for _, sec := range sg.Sections {
		for _, m := range sec.Measures {
			if len(m.Tokens) > 0 && !SilentBar(m.Tokens) && !allSimile(m.Tokens) {
				return true
			}
		}
	}
	return false
}

// SilentBar: a grid bar that names no chord at all — "?" (unknown), "."
// (nothing new), "R" (rest) or N.C.
func SilentBar(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, t := range tokens {
		switch strings.TrimSpace(t) {
		case "?", ".", "R", "N.C.", "NC":
		default:
			return false
		}
	}
	return true
}

// allSimile: a bar of nothing but "%" (the bar before, again).
func allSimile(tokens []string) bool {
	for _, t := range tokens {
		if strings.TrimSpace(t) != "%" {
			return false
		}
	}
	return true
}

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
