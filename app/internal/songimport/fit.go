package songimport

import (
	"app/internal/chartalign"
)

// Does a song follow its reference recording? (docs/ownership-model.md
// §3: a public song's structure is its recording's; flags only — a badge
// or a warning, never a rule.) Read from what the app already has: the
// recording's own analysis (its beats, bars and chords, cmd/stemsd,
// migration 0106) with the chart laid on it (chartalign, the Sync sheet's
// "Analysed" panel, #117–#119).
//
// The rule:
//   - "unknown" — the alignment isn't sure of the recording's beat
//     (rubato, live, no drums: state "unsure"), or the bars say the chart
//     counts in another octave (×2 / ÷2 offered first): no verdict.
//   - "differs" — the recording plays bars before the chart's first
//     section (an intro the chart lacks); or in all the chart's bar count
//     is far from the recording's ("bars", "too-long"), or many of its
//     bars aren't heard ("unplaced"); or a section found with confidence
//     (0.5 or more), not the last (which runs into a fade), has a bar
//     count other than the one the recording plays ("Chorus 2: 8 bars
//     written, 16 played"). Each is a FitReason.
//   - "follows" — otherwise: every section found where the chart puts
//     it, as long as the recording plays it.
//
// A chart that only transposes (a capo chart) or whose chords the
// recording contradicts here and there still follows: the structure is
// what's checked, not the chords.

// Recording fits.
const (
	FitFollows = "follows"
	FitDiffers = "differs"
	FitUnknown = "unknown"
)

// FitReason is one way the chart differs from its recording.
type FitReason struct {
	// Kind: "intro" (Bars played before the chart's first section),
	// "section" (Label written Written bars, played Played), "bars"
	// (in all, Written against Played), "unplaced" (many bars not heard).
	Kind    string `json:"kind"`
	Label   string `json:"label,omitempty"`
	Bars    int    `json:"bars,omitempty"`
	Written int    `json:"written,omitempty"`
	Played  int    `json:"played,omitempty"`
}

// Fit is RecordingFit's verdict.
type Fit struct {
	Fit     string      `json:"fit"`
	Reasons []FitReason `json:"reasons,omitempty"`
}

// RecordingFit lays body on rec (no hints: as detected, the octave the
// chart's tempo says) and says whether the chart follows the recording.
func RecordingFit(rec *chartalign.Recording, body string, sections []SyncSection) Fit {
	if rec == nil {
		return Fit{Fit: FitUnknown}
	}
	p := ProposeAutoSync(rec, body, sections, chartalign.Hints{})
	if p.Result == nil || p.State == "unsure" || p.SuggestTempoFactor != 0 || len(p.Sections) == 0 {
		return Fit{Fit: FitUnknown}
	}
	var why []FitReason
	if p.Intro != nil && p.Intro.Bars > 0 {
		why = append(why, FitReason{Kind: "intro", Bars: p.Intro.Bars})
	}
	switch p.Why {
	case "bars", "too-long":
		why = append(why, FitReason{Kind: "bars", Written: p.WrittenBars, Played: p.PlayedBars})
	case "unplaced":
		why = append(why, FitReason{Kind: "unplaced"})
	}
	for _, s := range p.Sections {
		if s.Last || s.Start < 0 || s.Confidence < 0.5 || s.Played == s.Written {
			continue
		}
		label := p.Labels[s.Index]
		if label == "" {
			label = s.Label
		}
		why = append(why, FitReason{Kind: "section", Label: label, Written: s.Written, Played: s.Played})
	}
	if len(why) > 0 {
		return Fit{Fit: FitDiffers, Reasons: why}
	}
	return Fit{Fit: FitFollows}
}
