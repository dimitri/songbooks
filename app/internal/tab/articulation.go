package tab

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// parseArticulation classifies a note's "^"label"" text (real LilyPond
// text-articulation syntax — see Beat.BendSemitones's own doc comment
// for why this subset repurposes it instead of inventing a new token)
// into either a bend or a palm mute. This vocabulary is this subset's
// OWN addition, not found anywhere in this app's real legacy corpus —
// added ahead of a planned Guitar Pro importer, which carries a
// per-note bend amount (in its own quarter-tone units) and palm-mute
// flag natively and needs some text form to round-trip through this
// package's LilyPond-subset source.
//
// Recognized labels:
//   - "PM" or "P.M." — palm mute (case-insensitive).
//   - A bend amount in the standard tab vocabulary: "full", "1/2",
//     "1/4", "3/4", a whole number of steps ("1", "2"), or a mixed
//     number ("1 1/2", "1 1/4") — one "step" is a whole tone (2
//     semitones), matching how every real tab chart and Guitar Pro
//     itself describes a bend's size.
//   - Either of the above followed by " (release)" marks BendRelease —
//     the string bends up then releases back down before the beat ends
//     (Guitar Pro's own "bend and release" type).
//
// An unrecognized label is an error, not a silent no-op: a typo here
// should fail loudly at parse time, not quietly produce an unmarked
// note.
func parseArticulation(label string) (bendSemitones float64, release, palmMute bool, err error) {
	s := strings.TrimSpace(label)
	upper := strings.ToUpper(s)
	if upper == "PM" || upper == "P.M." {
		return 0, false, true, nil
	}

	release = false
	if rest, ok := strings.CutSuffix(s, " (release)"); ok {
		release = true
		s = rest
	}

	if strings.EqualFold(s, "full") {
		return 2, release, false, nil
	}
	steps, err := parseStepFraction(s)
	if err != nil {
		return 0, false, false, fmt.Errorf("score: unrecognized note articulation %q: %w", label, err)
	}
	return steps * 2, release, false, nil
}

// BendLabel is parseArticulation's inverse — renders a bend back to the
// same tab-chart text a renderer displays above the bend arrow ("full",
// "1/2", "1 1/2 (release)", ...). Shared between the PDF and (if ever
// needed) web renderers so the displayed label always matches what the
// LilyPond-subset source actually says, rather than each renderer
// re-deriving its own rounding/spelling of the fraction.
func BendLabel(semitones float64, release bool) string {
	steps := semitones / 2
	whole := int(steps)
	frac := steps - float64(whole)
	var s string
	switch {
	case frac == 0:
		if whole == 1 {
			s = "full"
		} else {
			s = strconv.Itoa(whole)
		}
	case frac == 0.25:
		s = fracPart(whole, "1/4")
	case frac == 0.5:
		s = fracPart(whole, "1/2")
	case frac == 0.75:
		s = fracPart(whole, "3/4")
	default:
		s = strconv.FormatFloat(steps, 'g', -1, 64)
	}
	if release {
		s += " (release)"
	}
	return s
}

func fracPart(whole int, frac string) string {
	if whole == 0 {
		return frac
	}
	return strconv.Itoa(whole) + " " + frac
}

// stepFractionRE matches a whole number, a simple fraction, or a mixed
// number of "steps": "1", "1/2", "1 1/2".
var stepFractionRE = regexp.MustCompile(`^(?:(\d+)\s+)?(\d+)(?:/(\d+))?$`)

func parseStepFraction(s string) (float64, error) {
	m := stepFractionRE.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("not a bend amount (want \"full\", \"1/2\", \"1/4\", \"1 1/2\", ...)")
	}
	whole := 0.0
	if m[1] != "" {
		w, _ := strconv.Atoi(m[1])
		whole = float64(w)
	}
	num, _ := strconv.Atoi(m[2])
	if m[3] == "" {
		return whole + float64(num), nil
	}
	den, _ := strconv.Atoi(m[3])
	if den == 0 {
		return 0, fmt.Errorf("zero denominator in bend fraction %q", s)
	}
	return whole + float64(num)/float64(den), nil
}
