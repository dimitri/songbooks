package irealpro

import (
	"regexp"
	"strings"

	"app/internal/chordsym"
	"app/internal/song"
)

// This file is the section-aware iReal/UG merge: given an already-
// drafted .song source (from the UG importer — lyrics, section labels,
// usually one or two real grids) and a matched iReal Tune, it maps
// iReal's own section-letter segments onto the draft's section labels
// by appearance order and offers each a bar-accurate replacement grid.
//
// It deliberately NEVER silently replaces a transposed match: an
// ordered longest-common-subsequence check over chord roots still
// turned up two real, high-scoring false positives this session
// (Billie Jean, Wonderwall) once a transposition was involved — root-
// sequence matching alone isn't a strong enough signal to trust
// automatically when the keys disagree. Only an offset-0 match (no
// transposition needed) is ever returned as Safe==true.

var mergeSectionHeaderRE = regexp.MustCompile(`^\[([^\]]+)\](\s*\(repeat\))?\s*$`)
var mergeGridLineRE = regexp.MustCompile(`^grid:(.*)$`)
var mergeRootTokenRE = regexp.MustCompile(`\b([A-G][#b]?)\b`)

func rootSequence(s string) []string {
	var out []string
	for _, m := range mergeRootTokenRE.FindAllStringSubmatch(s, -1) {
		if len(out) == 0 || out[len(out)-1] != m[1] {
			out = append(out, m[1])
		}
	}
	return out
}

func transposeRootSeq(seq []string, semitones int) []string {
	out := make([]string, len(seq))
	for i, r := range seq {
		pc := chordsym.PitchClass(r)
		if pc < 0 {
			out[i] = r
			continue
		}
		out[i] = chordsym.SpellPitchClass(pc+semitones, false)
	}
	return out
}

func lcsRatio(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	shorter := len(a)
	if len(b) < shorter {
		shorter = len(b)
	}
	return float64(dp[len(a)][len(b)]) / float64(shorter)
}

func bestOffset(ourSeq []string, candidateGrid string) (offset int, score float64) {
	candSeq := rootSequence(candidateGrid)
	best, bestScore := 0, lcsRatio(ourSeq, candSeq)
	for _, off := range []int{1, -1, 2, -2, 3, -3} {
		s := lcsRatio(ourSeq, transposeRootSeq(candSeq, off))
		if s > bestScore {
			best, bestScore = off, s
		}
	}
	return best, bestScore
}

// Segments splits t's flattened Measures by its own iReal section
// letters into one simplified grid-text-per-DISTINCT-letter (first
// occurrence only — a repeated section reuses the same reference grid
// for every later occurrence of that label in the target draft).
func (t *Tune) Segments() []string {
	if len(t.Sections) == 0 {
		g := t.GridText()
		if g == "" {
			return nil
		}
		return []string{g}
	}
	seen := map[string]bool{}
	var out []string
	for i, b := range t.Sections {
		if seen[b.Letter] {
			continue
		}
		seen[b.Letter] = true
		end := len(t.Measures)
		if i+1 < len(t.Sections) {
			end = t.Sections[i+1].Index
		}
		if b.Index >= end {
			continue
		}
		out = append(out, song.SimplifyGridText(strings.Join(t.Measures[b.Index:end], " | ")))
	}
	if len(out) == 0 {
		g := t.GridText()
		if g != "" {
			out = []string{g}
		}
	}
	return out
}

func transposeGridText(grid string, semitones int) string {
	if semitones == 0 {
		return grid
	}
	toks := strings.Fields(grid)
	for i, t := range toks {
		if t == "|" || t == "%" || t == "N.C." || strings.ContainsAny(t, ":") {
			continue
		}
		toks[i] = chordsym.Transpose(t, semitones, false)
	}
	return strings.Join(toks, " ")
}

type draftSection struct {
	Label      string
	HeaderLine int
	GridLine   int // -1 if the section has no existing grid: line
	HasContent bool
}

func parseDraftSections(body string) (lines []string, sections []draftSection) {
	lines = strings.Split(body, "\n")
	for i, l := range lines {
		m := mergeSectionHeaderRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		sec := draftSection{Label: m[1], HeaderLine: i, GridLine: -1}
		for j := i + 1; j < len(lines) && mergeSectionHeaderRE.FindStringSubmatch(lines[j]) == nil; j++ {
			if mergeGridLineRE.MatchString(lines[j]) {
				sec.GridLine = j
			}
			trimmed := strings.TrimSpace(lines[j])
			if trimmed != "" && !strings.HasPrefix(trimmed, "note:") && !mergeGridLineRE.MatchString(lines[j]) {
				sec.HasContent = true
			}
		}
		sections = append(sections, sec)
	}
	return lines, sections
}

// SectionMergeCandidate is one of the draft's sections, with whatever
// iReal segment was mapped onto it (by appearance order) and the
// transposition needed to align it, if any — for the import wizard to
// show the user exactly what would change before they accept it.
type SectionMergeCandidate struct {
	Label        string
	CurrentGrid  string // "" if the section has no grid yet
	ProposedGrid string // the iReal segment, already transposed to match
	Offset       int    // semitones applied to reach ProposedGrid; 0 = no transposition needed
	Score        float64
	Safe         bool // true only when Offset==0 and Score is high enough to suggest one-click accept
}

// MergePreview maps t's segments onto draftBody's own sections (by
// appearance order) and scores each against the draft's own existing
// grid (the first section that has one, usually the Intro, is used as
// the reference for picking a transposition — see bestOffset). Returns
// one candidate per draft section that has lyric content, whether or
// not iReal had enough segments to cover all of them (extra sections
// reuse iReal's last segment, the common case for a vamp-based song
// whose groove runs through verse/chorus/bridge with one real
// harmonic idea).
func MergePreview(draftBody string, t *Tune) []SectionMergeCandidate {
	return MergePreviewSegments(draftBody, t.Segments())
}

// MergePreviewSegments is MergePreview's underlying implementation,
// taking the candidate chart's segments directly rather than a full
// Tune — a cache hit only ever has one already-flattened grid text,
// not a re-parseable Tune with real Sections, so this is what the
// import wizard's cached-lookup path calls into.
func MergePreviewSegments(draftBody string, segments []string) []SectionMergeCandidate {
	if len(segments) == 0 {
		return nil
	}
	lines, sections := parseDraftSections(draftBody)

	labelToSegment := map[string]string{}
	seenLabel := map[string]bool{}
	segIdx := 0
	for _, sec := range sections {
		if seenLabel[sec.Label] {
			continue
		}
		seenLabel[sec.Label] = true
		switch {
		case segIdx < len(segments):
			labelToSegment[sec.Label] = segments[segIdx]
			segIdx++
		default:
			labelToSegment[sec.Label] = segments[len(segments)-1]
		}
	}

	// Pick a transposition offset once, from whichever section already
	// has real chords (usually the Intro) — applied uniformly to every
	// segment, since they all come from the same tune/key.
	offset, score := 0, 0.0
	haveRef := false
	for _, sec := range sections {
		if sec.GridLine < 0 {
			continue
		}
		seg, ok := labelToSegment[sec.Label]
		if !ok {
			continue
		}
		ourSeq := rootSequence(lines[sec.GridLine])
		offset, score = bestOffset(ourSeq, seg)
		haveRef = true
		break
	}
	safe := haveRef && offset == 0 && score >= 0.6

	seenOut := map[string]bool{}
	var out []SectionMergeCandidate
	for _, sec := range sections {
		if seenOut[sec.Label] || !sec.HasContent {
			continue
		}
		seg, ok := labelToSegment[sec.Label]
		if !ok {
			continue
		}
		seenOut[sec.Label] = true
		current := ""
		if sec.GridLine >= 0 {
			current = strings.TrimPrefix(strings.TrimSpace(lines[sec.GridLine]), "grid:")
			current = strings.TrimSpace(current)
		}
		out = append(out, SectionMergeCandidate{
			Label: sec.Label, CurrentGrid: current,
			ProposedGrid: transposeGridText(seg, offset),
			Offset:       offset, Score: score, Safe: safe,
		})
	}
	return out
}

// ApplyMerge writes accepted candidates' ProposedGrid into draftBody —
// textual insertion/replacement only, exactly like the import wizard's
// own UG-draft handling: an existing grid: line is replaced, a missing
// one is inserted right after the section header; lyrics and every
// other line are untouched. acceptLabels selects which candidates (by
// Label) to actually apply; an empty/nil set applies none.
func ApplyMerge(draftBody string, candidates []SectionMergeCandidate, acceptLabels map[string]bool) string {
	lines, sections := parseDraftSections(draftBody)
	proposed := map[string]string{}
	for _, c := range candidates {
		if acceptLabels[c.Label] {
			proposed[c.Label] = c.ProposedGrid
		}
	}
	if len(proposed) == 0 {
		return draftBody
	}

	type edit struct {
		lineIdx  int
		gridLine int
		newGrid  string
	}
	var edits []edit
	applied := map[string]bool{}
	for _, sec := range sections {
		if applied[sec.Label] {
			continue
		}
		grid, ok := proposed[sec.Label]
		if !ok {
			continue
		}
		applied[sec.Label] = true
		edits = append(edits, edit{lineIdx: sec.HeaderLine, gridLine: sec.GridLine, newGrid: grid})
	}
	if len(edits) == 0 {
		return draftBody
	}

	newLines := append([]string(nil), lines...)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		gridText := "grid: " + e.newGrid
		if e.gridLine >= 0 {
			newLines[e.gridLine] = gridText
		} else {
			tail := append([]string{gridText}, newLines[e.lineIdx+1:]...)
			newLines = append(newLines[:e.lineIdx+1], tail...)
		}
	}
	return strings.Join(newLines, "\n")
}
