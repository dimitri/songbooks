// Package irealpro parses iReal Pro's "irealb://" chord-chart URLs — a
// single tune, or a whole library of tunes concatenated with "===" — into
// this app's own song.Measure grid shape.
//
// The URL's chord data is run through a simple, fixed character-shuffle
// cipher (not real encryption, no secret key) before it's usable text.
// That cipher (_unscramble50 below) and the chord-string grammar it
// unscrambles into (bar lines, repeat brackets, endings, codas, slash/
// repeat-measure shorthand) were originally worked out by the iReal Pro
// fan community (credited in several open-source parsers as "ironss and
// pianosnake") and are reimplemented here from that community
// documentation — most directly cross-checked against drs251/pyRealParser
// (MIT licensed) — not reverse-engineered from scratch in this session.
// This package only ever touches chord symbols and bar structure (never
// lyrics — iReal Pro charts don't have any), the same category of content
// internal/importer's own UG chord-grid extraction already handles.
package irealpro

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"app/internal/song"
)

// chordsPrefix marks the start of the actual (unscrambled) chord data
// within a tune's "=" separated field list — present verbatim, never
// itself scrambled, so it's also used as an anchor to locate the chords
// field regardless of how many metadata fields precede it (see Parse).
const chordsPrefix = "1r34LbKcu7"

// unscramble50 unshuffles one 50-character block: swap positions 0..4
// with 49..45, and swap positions 10..23 with 39..26. Ported verbatim
// from the community-documented algorithm (see package doc).
func unscramble50(block string) string {
	b := []rune(block)
	if len(b) != 50 {
		return block
	}
	out := make([]rune, 50)
	copy(out, b)
	for i := 0; i < 5; i++ {
		out[i], out[49-i] = b[49-i], b[i]
	}
	for i := 10; i < 24; i++ {
		out[i], out[49-i] = b[49-i], b[i]
	}
	return string(out)
}

// unscrambleChordString applies unscramble50 to every full 50-character
// block of a scrambled chord string, left as-is for the final partial
// block. A block is skipped (left scrambled, matching the reference
// implementation) when fewer than 2 characters remain after it — too
// short to matter and not worth a special case.
func unscrambleChordString(s string) string {
	var b strings.Builder
	for len(s) > 50 {
		block := s[:50]
		s = s[50:]
		if len(s) < 2 {
			b.WriteString(block)
		} else {
			b.WriteString(unscramble50(block))
		}
	}
	b.WriteString(s)
	return b.String()
}

var (
	newMeasureRE  = regexp.MustCompile(`LZ|K`)
	oneBarRepeat  = regexp.MustCompile(`cl`)
	doubleStarRE  = regexp.MustCompile(`\*\s*\*`)
	vSpacerRE     = regexp.MustCompile(`Y+`)
	emptySpaceRE  = regexp.MustCompile(`XyQ|,`)
	emptyMeasRE   = regexp.MustCompile(`\|\s*\|`)
	endMarkerRE   = regexp.MustCompile(`Z`)
	spaceAfterBar = regexp.MustCompile(`\|\s+`)
	multiSpaceRE  = regexp.MustCompile(`\s+`)
	// A "W" was observed (this session, real forum-fetched data) always
	// immediately before a "/" slash-bass marker, e.g. "Eb-7W/Gb" —
	// never as part of a real chord root (iReal roots are A-G only) —
	// and isn't documented in the reference implementation this package
	// was cross-checked against either; stripped as a harmless unknown
	// annotation rather than left to leak into the chord text.
	slashMarkerWRE = regexp.MustCompile(`W/`)
)

// cleanupChordString normalizes the unscrambled text into a plain,
// readable chord string: unifies the several raw symbols that all mean
// "new measure" or "no chord" into one, strips vertical-spacing/empty
// markers, and collapses whitespace.
func cleanupChordString(s string) string {
	s = slashMarkerWRE.ReplaceAllString(s, "/")
	s = newMeasureRE.ReplaceAllString(s, "|")
	s = oneBarRepeat.ReplaceAllString(s, "x")
	s = doubleStarRE.ReplaceAllString(s, "")
	s = vSpacerRE.ReplaceAllString(s, "")
	s = emptySpaceRE.ReplaceAllString(s, " ")
	s = emptyMeasRE.ReplaceAllString(s, "|")
	s = endMarkerRE.ReplaceAllString(s, "")
	s = spaceAfterBar.ReplaceAllString(s, "|")
	s = multiSpaceRE.ReplaceAllString(s, " ")
	return strings.TrimRight(s, " ")
}

var (
	bracketRE  = regexp.MustCompile(`[\[\]]`)
	commentRE  = regexp.MustCompile(`<.*?>`)
	altChordRE = regexp.MustCompile(`\([^)]*\)`)
	fermataRE  = regexp.MustCompile(`f`)
	lNotAltRE  = regexp.MustCompile(`(^|[^a])l([^t]|$)`)
	sNotSusRE  = regexp.MustCompile(`(^|[^u])s([^u]|$)`)
	// Captures the letter so removeAnnotations can turn it into a
	// section-boundary sentinel instead of just discarding it — see
	// getMeasuresWithSections.
	sectionRE   = regexp.MustCompile(`\*(\w)`)
	timeSigRE   = regexp.MustCompile(`T\d+`)
	augStarRE   = regexp.MustCompile(`\+\*`)
	markersRE   = regexp.MustCompile(`U|S|Q|N\d`)
	repeatBlkRE = regexp.MustCompile(`\{(.+?)\}`)
	firstEndRE  = regexp.MustCompile(`N(\d)`)
	barAfterRE  = regexp.MustCompile(`^\}\s*\|`)
	repEndRE    = regexp.MustCompile(`\|\s*N(\d)`)
)

// removeAnnotations strips comments, section markers, alternative
// chords, time signatures and a handful of single-letter notation
// artifacts (fermata, "small" marker) that don't affect the bar/chord
// content this app's grid cares about.
func removeAnnotations(s string) string {
	s = bracketRE.ReplaceAllString(s, "|")
	s = emptyMeasRE.ReplaceAllString(s, "|")
	s = commentRE.ReplaceAllString(s, "")
	s = altChordRE.ReplaceAllString(s, "")
	s = fermataRE.ReplaceAllString(s, "")
	s = lNotAltRE.ReplaceAllString(s, "$1$2")
	s = sNotSusRE.ReplaceAllString(s, "$1$2")
	// Turned into its own bracketed "measure" (never merged with real
	// chord content) rather than discarded, so getMeasuresWithSections
	// can locate where each of iReal's own section letters begins in
	// the final, fully-expanded (repeats/codas resolved) bar sequence —
	// the section-aware merge needs real boundaries, not just one long
	// flat chart.
	s = sectionRE.ReplaceAllString(s, "|§$1§|")
	s = timeSigRE.ReplaceAllString(s, "")
	s = augStarRE.ReplaceAllString(s, "+")
	return s
}

func removeMarkers(s string) string {
	return markersRE.ReplaceAllString(s, "")
}

// fillLongRepeats expands a "{...}" repeat bracket (with or without a
// first/second-ending "N1"/"N2" pair inside it) into the literal,
// repeated bar sequence it stands for — this app's own grid keeps a
// bracket's intent via Measure.Open/Close/RepeatCount instead of
// spelling bars out twice, but that reconstruction (see
// internal/song.SimplifyMeasures) needs the FLAT bar sequence as its
// input, same as iReal's own renderer ultimately plays.
func fillLongRepeats(s string) string {
	loc := repeatBlkRE.FindStringSubmatchIndex(s)
	if loc == nil {
		return s
	}
	inner := s[loc[2]:loc[3]]

	if numLoc := firstEndRE.FindStringIndex(inner); numLoc != nil {
		firstRepeat := firstEndRE.ReplaceAllString(inner, "")
		barLine := "|"
		if barAfterRE.MatchString(s[loc[1]-1:]) {
			barLine = ""
		}
		newS := s[:loc[0]] + "|" + firstRepeat + barLine + s[loc[1]:]

		// The saved repeat (for later endings) is whatever came before
		// the first "N\d" marker, with markers stripped.
		beforeN := inner[:strings.IndexAny(inner, "N")]
		// IndexAny found a byte position for "N", but we want the
		// match from firstEndRE itself to be safe with multi-digit
		// forms; recompute via the regex location instead.
		if m := regexp.MustCompile(`([^N]+)N\d`).FindStringSubmatch(inner); m != nil {
			beforeN = m[1]
		}
		repeat := removeMarkers(beforeN)

		for repEndRE.MatchString(newS) {
			newS = repEndRE.ReplaceAllString(newS, "|"+repeat)
		}
		return newS
	}

	// A plain repeat bracket with no alternate endings.
	newS := s[:loc[0]] + "|" + inner + " |" + removeMarkers(inner) + s[loc[1]:] + "|"
	return fillLongRepeats(newS)
}

// fillCodas flattens a D.C./D.S. al Coda marker pair ("Q"..."Q", with
// an optional segno "S") into its literal played-out bar sequence, same
// "give the simplifier real bars" reasoning as fillLongRepeats.
func fillCodas(s string) (string, error) {
	qs := strings.Count(s, "Q")
	if qs > 2 {
		return "", fmt.Errorf("could not parse codas: expected 0, 1, or 2 Qs, got %d", qs)
	}
	if qs == 1 {
		return strings.ReplaceAll(s, "Q", ""), nil
	}
	if qs == 2 {
		q1 := strings.IndexByte(s, 'Q')
		q2 := strings.IndexByte(s[q1+1:], 'Q') + q1 + 1
		// segno should mark where the repeated section starts, before
		// the first Q — a real forum-fetched library turned up a case
		// where 'S' instead occurs later in the string (not a genuine
		// segno at all, just some other use of the letter), which would
		// otherwise put segno past q1 and panic the s[segno+1:q1] slice
		// below; treat that as "no segno" rather than trust it blindly.
		segno := strings.IndexByte(s, 'S')
		if segno == -1 || segno >= q1 {
			segno = 0
		}
		coda := s[q2+1:]
		repeat := s[segno+1 : q1]
		newS := s[:q2] + repeat + " |" + coda
		newS = strings.NewReplacer("Q", "", "S", "").Replace(newS)
		return newS, nil
	}
	return s, nil
}

var chordTokenRE = regexp.MustCompile(`([A-GNn][^A-GN/]*(?:/[A-GN][#b]?)?)`)

// fillSingleDoubleRepeats resolves "x" (repeat previous bar) and "r"
// (repeat previous TWO bars) shorthand into literal chord content.
func fillSingleDoubleRepeats(measures []string) []string {
	for i := 1; i < len(measures); i++ {
		if measures[i] == "x" {
			measures[i] = removeMarkers(measures[i-1])
		}
	}
	for i := 2; i < len(measures); i++ {
		if measures[i] == "r" {
			first := removeMarkers(measures[i-2])
			second := removeMarkers(measures[i-1])
			tail := append([]string{first, second}, measures[i+1:]...)
			measures = append(measures[:i], tail...)
			i++
		}
	}
	return measures
}

// fillSlashes resolves a "p" slash marker (repeat the previous chord
// within a measure, e.g. "C / / /") to that literal chord symbol.
func fillSlashes(measures []string) []string {
	for i := 1; i < len(measures); i++ {
		for {
			slash := strings.IndexByte(measures[i], 'p')
			if slash == -1 {
				break
			}
			var prevChord string
			if slash == 0 {
				m := chordTokenRE.FindAllString(measures[i-1], -1)
				if len(m) > 0 {
					prevChord = m[len(m)-1]
				}
				measures[i] = prevChord + measures[i][1:]
			} else {
				m := chordTokenRE.FindAllString(measures[i][:slash], -1)
				if len(m) > 0 {
					prevChord = m[len(m)-1]
				}
				measures[i] = measures[i][:slash] + prevChord + measures[i][slash+1:]
			}
		}
	}
	return measures
}

func addSpaceBetweenChords(measures []string) []string {
	for i, m := range measures {
		measures[i] = strings.Join(chordTokenRE.FindAllString(m, -1), " ")
	}
	return measures
}

func replaceNoChords(measures []string) []string {
	for i, m := range measures {
		m = strings.ReplaceAll(m, "nn", "N.C.")
		m = strings.ReplaceAll(m, "n", "N.C.")
		measures[i] = m
	}
	return measures
}

var measureSplitRE = regexp.MustCompile(`\||LZ|K|Z|\{|\}|\[|\]`)

// getMeasures turns a cleaned/annotation-stripped chord string into a
// flat list of per-measure chord strings — every repeat bracket, coda
// and shorthand already resolved to literal content (see
// fillLongRepeats/fillCodas/fillSingleDoubleRepeats/fillSlashes), empty
// measures dropped.
func getMeasures(chordString string) ([]string, error) {
	measures, _, err := getMeasuresWithSections(chordString)
	return measures, err
}

// sectionBoundary marks where one of iReal's own section letters (*A,
// *B, *i for intro, ...) begins in the fully-expanded (repeats/codas
// already resolved) measure list — Index is approximate when a "r"
// (two-bar repeat) shorthand later in the same tune grows the array,
// since boundaries are recorded against the pre-expansion index space;
// good enough for carving a chart into natural chunks (the section-
// aware merge's actual use), not claimed to be sample-accurate.
type sectionBoundary struct {
	Letter string
	Index  int
}

var sectionTokenRE = regexp.MustCompile(`^§(\w)§$`)

func getMeasuresWithSections(chordString string) ([]string, []sectionBoundary, error) {
	s := cleanupChordString(chordString)
	s = removeAnnotations(s)
	s = fillLongRepeats(s)
	s, err := fillCodas(s)
	if err != nil {
		return nil, nil, err
	}
	raw := measureSplitRE.Split(s, -1)
	var measures []string
	var bounds []sectionBoundary
	for _, m := range raw {
		m = strings.ReplaceAll(m, " ", "")
		if m == "" {
			continue
		}
		if mm := sectionTokenRE.FindStringSubmatch(m); mm != nil {
			bounds = append(bounds, sectionBoundary{Letter: mm[1], Index: len(measures)})
			continue
		}
		measures = append(measures, m)
	}
	measures = fillSingleDoubleRepeats(measures)
	measures = fillSlashes(measures)
	measures = addSpaceBetweenChords(measures)
	measures = replaceNoChords(measures)
	return measures, bounds, nil
}

var timeSigRE2 = regexp.MustCompile(`T(\d)(\d)`)

func getTimeSignature(chordString string) (num, den int) {
	m := timeSigRE2.FindStringSubmatch(chordString)
	if m == nil {
		return 4, 4
	}
	num, _ = strconv.Atoi(m[1])
	den, _ = strconv.Atoi(m[2])
	return num, den
}

// Tune is one parsed iReal Pro chart — chords and bar structure only,
// the same "never lyrics, iReal charts don't have any" scope the
// package doc describes.
type Tune struct {
	Title, Composer, Style, Key string
	TimeSigNum, TimeSigDen      int
	ChordString                 string   // cleaned, human-readable chord text
	Measures                    []string // one entry per bar, already flattened (repeats/codas/shorthand resolved)
	// Sections marks where iReal's own section letters (*A, *B, *i for
	// intro, ...) begin within Measures — see sectionBoundary. iReal
	// doesn't name these "Verse"/"Chorus" the way this app's own
	// sections are labeled, just generic letters in appearance order;
	// matching them up to this app's own section labels is the caller's
	// job (see the section-aware merge), this package only exposes the
	// boundaries themselves.
	Sections []sectionBoundary
}

// splitFields splits a tune's "=" separated field list the same lossy
// way the reference implementation does (one-or-more consecutive "="
// collapse to a single separator, so a genuinely empty field and "one
// fewer field" are indistinguishable) — kept consistent with that
// known-working reference rather than "fixed" in a way that would
// silently diverge from it.
var fieldSplitRE = regexp.MustCompile(`=+`)

// parseTuneSafe wraps parseTune with a panic recovery — real forum-
// fetched data has already turned up at least one malformed tune that
// panicked deep in the grammar logic (an out-of-range slice in
// fillCodas, now fixed, but this package processes thousands of
// unpredictable real-world strings from a format with no spec to
// validate against, so one more surprising edge case shouldn't be able
// to take down a whole library's worth of otherwise-good songs).
func parseTuneSafe(tuneString string) (t *Tune, err error) {
	defer func() {
		if r := recover(); r != nil {
			t, err = nil, fmt.Errorf("panic parsing tune: %v", r)
		}
	}()
	return parseTune(tuneString)
}

// parseTune parses one tune's field-list string (already split off a
// "===" boundary, not yet containing the "irealb://" scheme or any
// playlist-level prefix — see ParseURL's stripPlaylistPrefix). Fields
// are title, composer, style, key, then the chords field (located by
// its unmistakable chordsPrefix anchor rather than assumed to sit at a
// fixed index, since a genuinely empty composer collapses away under
// fieldSplitRE's lossy "=+" splitting and shifts every later field left
// by one — confirmed against real forum-fetched tunes this session,
// most of which have an empty composer).
func parseTune(tuneString string) (*Tune, error) {
	fields := fieldSplitRE.Split(tuneString, -1)
	chordsIdx := -1
	for i, f := range fields {
		if strings.HasPrefix(f, chordsPrefix) {
			chordsIdx = i
			break
		}
	}
	if chordsIdx < 1 {
		return nil, fmt.Errorf("couldn't locate chord data in tune fields: %v", fields)
	}
	var title, composer, style, key string
	switch chordsIdx {
	case 1: // title, (composer+style+key all empty), chords
		title = fields[0]
	case 2: // title, style, key collapsed oddly — treat as title+style
		title, style = fields[0], fields[1]
	case 3: // title, style, key, chords — composer was empty
		title, style, key = fields[0], fields[1], fields[2]
	default: // title, composer, style, key, chords (the full form)
		title, composer, style, key = fields[0], fields[1], fields[2], fields[3]
	}

	scrambled := strings.TrimPrefix(fields[chordsIdx], chordsPrefix)
	raw := unscrambleChordString(scrambled)
	cleaned := cleanupChordString(raw)
	numSig, denSig := getTimeSignature(cleaned)
	measures, bounds, err := getMeasuresWithSections(cleaned)
	if err != nil {
		return nil, fmt.Errorf("tune %q: %w", title, err)
	}

	return &Tune{
		Title: strings.TrimSpace(title), Composer: strings.TrimSpace(composer),
		Style: strings.TrimSpace(style), Key: strings.TrimSpace(key),
		TimeSigNum: numSig, TimeSigDen: denSig,
		ChordString: cleaned, Measures: measures, Sections: bounds,
	}, nil
}

// playlistPrefixRE matches a short all-caps sentinel some forum-shared
// multi-tune "irealb://" links carry right before the first tune's
// title, with no "===" boundary of its own (confirmed this session
// against a real 404-song forum library link, where every tune parsed
// cleanly except the first, which had a stray "ABC=" glued onto its
// title field). Not documented anywhere found — inferred from that one
// real sample, so this intentionally only strips an all-caps token of
// 2-6 letters immediately followed by "=", never anything that could
// plausibly be a real (mixed-case) song title.
var playlistPrefixRE = regexp.MustCompile(`^[A-Z]{2,6}=`)

func stripPlaylistPrefix(body string) string {
	return playlistPrefixRE.ReplaceAllString(body, "")
}

// ParseURL parses one or more tunes out of an "irealb://..." URL (a
// single tune, or a whole library with tunes joined by "===").
func ParseURL(raw string) ([]*Tune, error) {
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		decoded = raw // best-effort: some real links aren't fully percent-escaped
	}
	const scheme = "irealb://"
	idx := strings.Index(decoded, scheme)
	if idx == -1 {
		return nil, fmt.Errorf("not an irealb:// url")
	}
	body := decoded[idx+len(scheme):]
	body = stripPlaylistPrefix(body)
	var tunes []*Tune
	for _, chunk := range strings.Split(body, "===") {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		t, err := parseTuneSafe(chunk)
		if err != nil {
			continue // one bad tune in a library of hundreds shouldn't fail the rest
		}
		tunes = append(tunes, t)
	}
	if len(tunes) == 0 {
		return nil, fmt.Errorf("no tunes parsed from this url")
	}
	return tunes, nil
}

// GridText renders this tune's flattened bar sequence as this app's own
// grid: line text, folding repeated bar-groups into "|: ... :|"/"%"
// notation via song.SimplifyGridText — the same compaction the UG
// importer now applies (see internal/importer's UGSection.Grid), so an
// iReal-sourced chart reads the same clean chart style a hand-written
// one does, not a literal bar-for-bar transcription of iReal's own
// already-flattened repeat/coda expansion.
func (t *Tune) GridText() string {
	if len(t.Measures) == 0 {
		return ""
	}
	return song.SimplifyGridText(strings.Join(t.Measures, " | "))
}
