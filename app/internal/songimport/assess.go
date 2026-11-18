package songimport

import (
	"fmt"
	"strings"

	"app/internal/chordsym"
	"app/internal/importer"
	"app/internal/song"
)

// RecordingState is what the song's reference recording is at: whether
// there is one, its voice/backing split's job status ("", "queued",
// "running", "done", "failed") and how many section markers it has.
type RecordingState struct {
	Has        bool
	StemStatus string
	Markers    int
}

// Assessment says what a saved song needs from the import wizard.
type Assessment struct {
	Verdict      string   `json:"verdict"` // "new" (nothing yet), "improve", "restart"
	Summary      []string `json:"summary"` // short facts, never lyrics
	Reasons      []string `json:"reasons"` // why this verdict
	Steps        []string `json:"steps"`   // wizard steps worth doing, in order: find, review, lyrics, recording, sync
	Sections     int      `json:"sections"`
	WithGrid     int      `json:"withGrid"`
	Sung         int      `json:"sung"`
	WithLyrics   int      `json:"withLyrics"`
	CleanupNotes int      `json:"cleanupNotes"`
}

// Assessment verdicts.
const (
	VerdictNew     = "new"
	VerdictImprove = "improve"
	VerdictRestart = "restart"
)

// Restart thresholds, conservative on purpose: a song map with its
// structure and a couple of grids is always worth improving, so only a
// chart that is mostly junk, or has nothing usable at all, says restart.
const (
	// junkMinLines/junkShare: CleanSongText removes at least this many
	// lines and at least this share of the section lines.
	junkMinLines = 8
	junkShare    = 1.0 / 3
	// badTokenMin/badTokenShare: at least this many grid tokens, and at
	// least this share of them are neither chords nor marks (%, ., N.C.…).
	badTokenMin   = 8
	badTokenShare = 0.5
	// keyFitMin/keyFitChords: with iReal Pro knowing the key, at least
	// this many distinct chords of which fewer than keyFitMin belong to
	// any one key (see keyFit): no key at all.
	keyFitChords = 4
	keyFitMin    = 0.5
)

// sungKinds are the section kinds that carry lyrics; a section of
// another kind counts as sung only when it has lyrics.
var sungKinds = map[string]bool{"verse": true, "chorus": true, "pre-chorus": true, "post-chorus": true, "bridge": true}

// Assess looks at a saved song's text, its recording and iReal Pro's key
// for it ("" when unknown) and says whether to start from nothing
// ("new"), work on what's there ("improve") or start over ("restart"),
// and which wizard steps are worth doing.
//
// Verdicts:
//   - new: no section has a grid, lyrics or notation, and there are at
//     most one section and no map: header (an empty song or a
//     placeholder).
//   - restart: sections but no grid and no lyrics anywhere; or cleanup
//     would remove at least junkMinLines lines and a third of the
//     sections' lines; or at least half of badTokenMin+ grid tokens are
//     not chords; or iReal Pro knows the key and fewer than half of the
//     chart's (4+) distinct chords belong to any one key.
//   - improve: anything else.
//
// Steps (improve): find and review when a kind of section has no grid
// in its first occurrence (a later bare occurrence repeats the first;
// a bare Solo plays over the changes and a bare Outro ends the song, so
// neither counts); lyrics when a sung section lacks them (a bare repeat
// of a kind already seen counts once, with that kind); recording when
// there is none or its split failed; sync when there is one but no
// section markers. new and restart always list find, review and lyrics.
func Assess(body string, rec RecordingState, irealKey string) Assessment {
	var a Assessment
	sg := song.Parse(body)
	a.Sections = len(sg.Sections)

	// Sections: grids, sung sections, lyrics.
	firstSeen := map[string]bool{}
	var noGrid, carryOn []string
	noGridSeen := map[string]bool{}
	inherit, before := 0, 0
	anyGrid, anyLyrics := false, false
	for _, sec := range sg.Sections {
		grid := len(sec.Measures) > 0 || len(sec.Scores) > 0 || len(sec.GP) > 0
		lyrics := len(sec.Lyrics) > 0
		bare := !grid && !lyrics
		kind := sectionKey(sec.Label)
		if grid {
			a.WithGrid++
			anyGrid = true
		} else if anyGrid {
			inherit++
		} else {
			before++
		}
		if lyrics {
			anyLyrics = true
		}
		first := !firstSeen[kind]
		firstSeen[kind] = true
		// A first section with no grid of its own is missing one only
		// when nothing before it has a grid: after a gridded Intro, a
		// song map's bare Verse means its chords carry on.
		if first && !grid && !sec.Repeat {
			k := importer.SectionKind(sec.Label)
			switch {
			case k == "solo" || (k == "outro" && bare) || noGridSeen[kind]:
			case anyGrid:
				carryOn = append(carryOn, strings.TrimSpace(sec.Label))
			default:
				noGridSeen[kind] = true
				noGrid = append(noGrid, strings.TrimSpace(sec.Label))
			}
		}
		sung := lyrics || sungKinds[importer.SectionKind(sec.Label)]
		if sung && !sec.Repeat && !(bare && !first) {
			a.Sung++
			if lyrics {
				a.WithLyrics++
			}
		}
	}

	cleaned, notes := importer.CleanSongText(body, importer.CleanOptions{})
	a.CleanupNotes = len(notes)
	removed := sectionLines(body) - sectionLines(cleaned)
	total := sectionLines(body)

	tokens, bad := gridTokens(sg)
	chords := song.UsedChords(sg)
	fit := keyFit(chords)

	// Summary.
	s := fmt.Sprintf("%d sections, %d with their own grid", a.Sections, a.WithGrid)
	if inherit > 0 {
		s += fmt.Sprintf(", %d repeat one", inherit)
	}
	if before > 0 {
		s += fmt.Sprintf(", %d before any grid", before)
	}
	a.Summary = append(a.Summary, s)
	if len(carryOn) > 0 {
		a.Summary = append(a.Summary, "chords carried on from earlier: "+strings.Join(carryOn, ", "))
	}
	if len(noGrid) > 0 {
		a.Summary = append(a.Summary, "no grid of its own: "+strings.Join(noGrid, ", "))
	}
	if a.Sung > 0 {
		a.Summary = append(a.Summary, fmt.Sprintf("lyrics in %d of %d sung sections", a.WithLyrics, a.Sung))
	} else {
		a.Summary = append(a.Summary, "no sung sections")
	}
	for _, k := range []string{"key", "tempo", "time"} {
		if strings.TrimSpace(sg.Get(k)) == "" {
			a.Summary = append(a.Summary, "no "+k)
		}
	}
	switch {
	case !rec.Has:
		a.Summary = append(a.Summary, "no reference recording")
	case rec.StemStatus == "failed":
		a.Summary = append(a.Summary, "recording: splitting the voice failed")
	case rec.StemStatus != "done":
		a.Summary = append(a.Summary, "recording: voice not split yet")
	}
	if rec.Has {
		if rec.Markers == 0 {
			a.Summary = append(a.Summary, "no section markers on the recording")
		} else {
			a.Summary = append(a.Summary, fmt.Sprintf("%d section markers on the recording", rec.Markers))
		}
	}
	if a.CleanupNotes > 0 {
		a.Summary = append(a.Summary, fmt.Sprintf("%d cleanup notes", a.CleanupNotes))
	}
	if irealKey != "" && anyGrid {
		if k := CheckKey(body, irealKey, "", 0); k.Chart != "" && !k.IRealAgrees {
			a.Summary = append(a.Summary, fmt.Sprintf("key: the chart says %s (%s), iReal Pro %s", k.Chart, k.ChartFrom, irealKey))
		}
	}

	// Verdict.
	switch {
	case !anyGrid && !anyLyrics && a.Sections <= 1 && strings.TrimSpace(sg.Get("map")) == "":
		a.Verdict = VerdictNew
		a.Reasons = append(a.Reasons, "no grid, lyrics or structure yet")
	default:
		if !anyGrid && !anyLyrics {
			a.Reasons = append(a.Reasons, "sections, but no grid and no lyrics in any")
		}
		if removed >= junkMinLines && float64(removed) >= junkShare*float64(total) {
			a.Reasons = append(a.Reasons, fmt.Sprintf("cleanup would remove %d of its %d lines", removed, total))
		}
		if tokens >= badTokenMin && float64(bad) >= badTokenShare*float64(tokens) {
			a.Reasons = append(a.Reasons, fmt.Sprintf("%d of %d grid tokens aren't chords", bad, tokens))
		}
		if irealKey != "" && len(chords) >= keyFitChords && fit < keyFitMin {
			a.Reasons = append(a.Reasons, fmt.Sprintf("its chords fit no key (%.0f%% at best) while iReal Pro says %s", fit*100, irealKey))
		}
		if len(a.Reasons) > 0 {
			a.Verdict = VerdictRestart
		} else {
			a.Verdict = VerdictImprove
		}
	}

	// Steps.
	fresh := a.Verdict != VerdictImprove
	if fresh || len(noGrid) > 0 {
		a.Steps = append(a.Steps, "find", "review")
		if !fresh {
			a.Reasons = append(a.Reasons, "grids missing: "+strings.Join(noGrid, ", "))
		}
	}
	if fresh || a.WithLyrics < a.Sung {
		a.Steps = append(a.Steps, "lyrics")
		if !fresh {
			a.Reasons = append(a.Reasons, fmt.Sprintf("lyrics missing in %d sung sections", a.Sung-a.WithLyrics))
		}
	}
	if !rec.Has || rec.StemStatus == "failed" {
		a.Steps = append(a.Steps, "recording")
		if !fresh {
			a.Reasons = append(a.Reasons, "no usable reference recording")
		}
	}
	if rec.Has && rec.Markers == 0 {
		a.Steps = append(a.Steps, "sync")
		if !fresh {
			a.Reasons = append(a.Reasons, "the recording has no section markers")
		}
	}
	if a.Verdict == VerdictImprove && len(a.Reasons) == 0 {
		a.Reasons = append(a.Reasons, "nothing missing")
	}
	return a
}

// sectionKey groups sections of one kind: SectionKind, else the label's
// words without numbers.
func sectionKey(label string) string {
	if k := importer.SectionKind(label); k != "" {
		return k
	}
	var words []string
	for _, w := range strings.Fields(strings.ToLower(label)) {
		if strings.Trim(w, "0123456789") != "" {
			words = append(words, w)
		}
	}
	return "other:" + strings.Join(words, " ")
}

// sectionLines counts the non-blank lines from the first section header
// on: what CleanSongText may remove.
func sectionLines(body string) int {
	n, in := 0, false
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			in = true
			continue
		}
		if in && t != "" {
			n++
		}
	}
	return n
}

// gridMarks are grid tokens that are not chords and not junk.
var gridMarks = map[string]bool{
	"%": true, "%%": true, ".": true, "/": true, "-": true, "_": true, "x": true, "~": true,
	"R": true, "r": true, "N.C.": true, "NC": true, "N.C": true, "n.c.": true, "*": true,
}

// gridTokens counts a song's grid tokens and those that are neither a
// chord nor a mark.
func gridTokens(sg *song.Song) (total, bad int) {
	for _, sec := range sg.Sections {
		for _, m := range sec.Measures {
			for _, tok := range m.Tokens {
				total++
				t := strings.Trim(song.TokenChord(strings.TrimLeft(tok, "~0123456789:")), "()")
				if t == "" || gridMarks[t] || chordsym.IsChord(t) {
					continue
				}
				bad++
			}
		}
	}
	return total, bad
}

// keyFit is the best share, over every major and minor key, of chords
// that belong to it: a diatonic root with the degree's triad quality (a
// major V in minor too), or a diminished, half-diminished or augmented
// chord on any diatonic root. 1 when there are no chords.
func keyFit(chords []string) float64 {
	type ch struct {
		pc     int
		marker chordsym.Marker
	}
	var list []ch
	for _, c := range chords {
		p := chordsym.Split(c)
		if pc := chordsym.PitchClass(p.Root); pc >= 0 {
			list = append(list, ch{pc, chordsym.DecomposeQuality(p.Quality).Marker})
		}
	}
	if len(list) == 0 {
		return 1
	}
	// Degree -> its triad is minor (as in ChordKey).
	majorDeg := map[int]bool{0: false, 2: true, 4: true, 5: false, 7: false, 9: true, 11: true}
	minorDeg := map[int]bool{0: true, 2: true, 3: false, 5: true, 7: false, 8: false, 10: false, 11: true}
	best := 0
	for tonic := 0; tonic < 12; tonic++ {
		for _, deg := range []map[int]bool{majorDeg, minorDeg} {
			n := 0
			for _, c := range list {
				isMinor, ok := deg[(c.pc-tonic+12)%12]
				switch {
				case !ok:
				case c.marker == chordsym.MarkerDim || c.marker == chordsym.MarkerHalfDim || c.marker == chordsym.MarkerAug:
					n++
				case (c.marker == chordsym.MarkerMinor) == isMinor:
					n++
				}
			}
			if n > best {
				best = n
			}
		}
	}
	return float64(best) / float64(len(list))
}
