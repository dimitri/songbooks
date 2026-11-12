package song

import (
	"os"
	"regexp"
	"strconv"
	"strings"

	"app/internal/chordsym"
)

// sectionHeaderRE matches a line that is ENTIRELY one "[Label]" or
// "[Label] (repeat)" marker and nothing else — critically, the label
// itself may not contain a further "[" or "]", so a lyric line that
// merely happens to start and end with a bracket (a chord at its very
// start, a bare trailing chord with no following word at its very end —
// both routine in imported content, e.g. "[Em]Leading m[Am]e, going
// [G7]home [C]") is never misread as a section header. A real
// prefix/suffix check here is too loose for that — see this project's
// own history of exactly this bug biting UG-tab and LaTeX-songs-package
// imports before it was tightened.
var sectionHeaderRE = regexp.MustCompile(`^\[([^\[\]]+)\](\s*\(repeat\))?$`)

// ParseFile reads and parses a .song file.
func ParseFile(path string) (*Song, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(data)), nil
}

// rawSection accumulates one section's body while scanning the file, before
// its grid/lyric text is parsed into structured Measures/LyricLines.
type rawSection struct {
	label   string
	repeat  bool
	time    string
	scores  []rawScore
	times   int
	bars    []int
	gridRaw strings.Builder
	lines   []string
	notes   []string
}

// rawScore accumulates one "score[label]: ..." block's lines while
// scanning — see scoreBlockPrefix's own doc comment for how a block's
// extent is found.
type rawScore struct {
	label string
	lines []string
}

// Parse parses .song source text, following v2/lib/song.typ's parse-song.
func Parse(src string) *Song {
	song := &Song{}
	var cur *rawSection
	inHead := true

	flush := func() {
		if cur == nil {
			return
		}
		sec := Section{
			Label:       cur.label,
			Repeat:      cur.repeat,
			Time:        cur.time,
			Times:       cur.times,
			BarsPerLine: cur.bars,
			Lyrics:      make([]LyricLine, 0, len(cur.lines)),
			Notes:       cur.notes,
		}
		for _, rs := range cur.scores {
			sec.Scores = append(sec.Scores, ScoreVoice{Label: rs.label, Body: strings.Join(rs.lines, "\n")})
		}
		if spec := strings.TrimSpace(cur.gridRaw.String()); spec != "" {
			sec.Measures = parseGrid(spec)
		}
		for _, l := range cur.lines {
			sec.Lyrics = append(sec.Lyrics, LyricLine{Segments: parseLyric(l)})
		}
		song.Sections = append(song.Sections, sec)
	}

	// curScore/scoreBraceDepth track a "score[label]:" block currently
	// being captured — see scoreContinues' own doc comment for the
	// termination rule. Reset (curScore = nil) on every section header
	// and whenever a block's own extent ends, so a stale pointer from an
	// earlier section/block is never written to (always re-acquired,
	// after any append, right before use — see below).
	var curScore *rawScore
	var scoreBraceDepth int

	// Every diagram line in source order; split into the song's own set
	// and other keys' sets once the whole header (and so "key:") is read.
	var diagrams []ChordRef

	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}

		if m := sectionHeaderRE.FindStringSubmatch(line); m != nil {
			flush()
			cur = &rawSection{label: m[1], repeat: m[2] != ""}
			inHead = false
			curScore = nil
			continue
		}

		if inHead {
			if line == "" {
				continue
			}
			if key, rest, ok := splitChordLine(line); ok {
				if ref, ok := parseChordRef(rest); ok {
					ref.Key = key
					diagrams = append(diagrams, ref)
				}
				continue
			}
			if i := strings.Index(line, ":"); i >= 0 {
				k := strings.TrimSpace(line[:i])
				if isMetaKey(k) {
					song.Set(k, strings.TrimSpace(line[i+1:]))
				}
			}
			continue
		}

		if cur == nil {
			continue
		}

		// Checked before the blank-line skip below: a blank line (or the
		// next recognized directive) is exactly what ends an open block
		// when it isn't inside a balanced "{ }" — this needs to see it to
		// know capture has stopped, not have it silently skipped out from
		// under it.
		if curScore != nil {
			if scoreBraceDepth > 0 || scoreContinues(line) {
				curScore.lines = append(curScore.lines, line)
				scoreBraceDepth += strings.Count(line, "{") - strings.Count(line, "}")
				if scoreBraceDepth < 0 {
					scoreBraceDepth = 0
				}
				continue
			}
			curScore = nil
		}

		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "time:"):
			cur.time = strings.TrimSpace(line[5:])
		case strings.HasPrefix(line, "score["):
			if label, val, ok := parseScoreHeader(line); ok {
				cur.scores = append(cur.scores, rawScore{label: label})
				curScore = &cur.scores[len(cur.scores)-1]
				scoreBraceDepth = 0
				if val != "" {
					curScore.lines = append(curScore.lines, val)
					scoreBraceDepth = strings.Count(val, "{") - strings.Count(val, "}")
					if scoreBraceDepth < 0 {
						scoreBraceDepth = 0
					}
				}
			}
		case strings.HasPrefix(line, "grid:"):
			cur.gridRaw.WriteString(" " + strings.TrimSpace(line[5:]))
		case strings.HasPrefix(line, "bars:"):
			cur.bars = parseBars(strings.TrimSpace(line[5:]))
		case strings.HasPrefix(line, "times:"):
			if n, err := strconv.Atoi(strings.TrimSpace(line[6:])); err == nil {
				cur.times = n
			}
		case strings.HasPrefix(line, "note:"):
			cur.notes = append(cur.notes, strings.TrimSpace(line[5:]))
		case strings.HasPrefix(line, "score:"):
			// The old single-line "score: <label>" form (a Postgres-
			// stored-content pointer) — retired in favor of
			// "score[label]: <content>" (see ScoreVoice's own doc
			// comment, song.go): dropped rather than kept as a lyric
			// line, which is what the default case below would
			// otherwise do with it.
		case strings.HasPrefix(line, "|") && len(cur.lines) == 0:
			// Grid lines always precede lyric lines within a section; once
			// lyric text has started, a "|"-leading line is lyric content.
			cur.gridRaw.WriteString(" " + line)
		default:
			cur.lines = append(cur.lines, line)
		}
	}
	flush()

	songKey := song.Get("key")
	for _, ref := range diagrams {
		if ref.Key == "" || chordsym.SameKey(ref.Key, songKey) {
			song.Chords = append(song.Chords, ref)
		} else {
			song.OtherChords = append(song.OtherChords, ref)
		}
	}

	return song
}

// splitChordLine recognizes a diagram header line: "chord: Name Spec"
// (key "", the song's own key) or "chord[K]: Name Spec" (entered for key
// K). rest is what follows the colon.
func splitChordLine(line string) (key, rest string, ok bool) {
	if strings.HasPrefix(line, "chord:") {
		return "", line[len("chord:"):], true
	}
	if !strings.HasPrefix(line, "chord[") {
		return "", "", false
	}
	end := strings.Index(line, "]:")
	if end < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[len("chord["):end]), line[end+2:], true
}

// parseScoreHeader splits a "score[label]: value" line into its label
// and the (possibly empty) first-line content after the colon. ok is
// false for a malformed line (no closing "]") — left out of cur.scores
// entirely rather than guessed at.
func parseScoreHeader(line string) (label, val string, ok bool) {
	rest := line[len("score["):]
	end := strings.IndexByte(rest, ']')
	if end < 0 {
		return "", "", false
	}
	label = strings.TrimSpace(rest[:end])
	val = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest[end+1:]), ":"))
	return label, val, true
}

// scoreContinues reports whether line should be treated as more content
// for an IN-PROGRESS "score[label]:" block that's currently at brace
// depth 0 (tracked by the caller, Parse's own main loop) — inside a
// balanced "{ }" a line always continues regardless of what it looks
// like, since real LilyPond content can legitimately contain a blank
// line, or text that merely resembles another field, inside a multi-line
// "\new ... { ... }" body. At depth 0 (the common case for the bare
// "score[drum]: bd4 hh8 ..." shortcut form — see internal/tab.ParseVoice
// — which has no braces to track at all), a block ends at the first line
// that looks like the start of something else: blank, a new "[Section]"
// header, or another recognized field prefix — so a short, undecorated
// voice doesn't accidentally swallow whatever comes after it in the
// file.
func scoreContinues(line string) bool {
	if line == "" || sectionHeaderRE.MatchString(line) {
		return false
	}
	for _, p := range [...]string{"time:", "grid:", "bars:", "times:", "note:", "score:", "score["} {
		if strings.HasPrefix(line, p) {
			return false
		}
	}
	return true
}

// parseChordRef splits a "chord:" line's value into "Name Spec" — the
// chord name (first whitespace-delimited token, same grammar as a
// grid:/lyric chord symbol, never containing a space) and the raw
// fretboard-diagram spec (everything after it, trimmed). A line with no
// spec at all ("chord: Cm") is dropped rather than kept as a nameless
// diagram.
func parseChordRef(v string) (ChordRef, bool) {
	v = strings.TrimSpace(v)
	i := strings.IndexAny(v, " \t")
	if i < 0 {
		return ChordRef{}, false
	}
	name := v[:i]
	spec := strings.TrimSpace(v[i+1:])
	if name == "" || spec == "" {
		return ChordRef{}, false
	}
	return ChordRef{Name: name, Spec: spec}, true
}

func parseBars(v string) []int {
	if v == "" {
		return nil
	}
	var out []int
	for _, part := range strings.Split(v, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// parseGrid turns "|: Gm | Bb . D . :|" into measures, mirroring
// v2/lib/song.typ's parse-grid — minus transposition, which this package
// never performs.
func parseGrid(spec string) []Measure {
	var out []Measure
	for _, raw := range strings.Split(strings.TrimSpace(spec), "|") {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			continue
		}

		opens := strings.HasPrefix(tok, ":")
		closes := strings.HasSuffix(tok, ":")
		var repeatCount int
		if opens {
			tok = tok[1:]
			// An explicit repeat count rides right after the opening
			// colon, no space ("|:4 F#m | ..."), same place a real
			// chart would write "x4" next to the repeat sign — digits
			// consumed up front, before the usual TrimSpace, so a
			// count-less "|: ..." (the overwhelmingly common case) is
			// completely unaffected.
			j := 0
			for j < len(tok) && tok[j] >= '0' && tok[j] <= '9' {
				j++
			}
			if j > 0 {
				if n, err := strconv.Atoi(tok[:j]); err == nil {
					repeatCount = n
				}
				tok = tok[j:]
			}
			tok = strings.TrimSpace(tok)
		}
		if closes && len(tok) > 0 {
			tok = strings.TrimSpace(tok[:len(tok)-1])
		}

		var meter string
		if strings.HasPrefix(tok, "{") {
			if e := strings.Index(tok, "}"); e >= 0 {
				meter = tok[1:e]
				tok = strings.TrimSpace(tok[e+1:])
			}
		}

		var volta string
		if strings.HasPrefix(tok, "(") {
			if e := strings.Index(tok, ")"); e >= 0 {
				volta = tok[1:e]
				tok = strings.TrimSpace(tok[e+1:])
			}
		}

		if tok == "" && !opens && !closes {
			continue
		}

		var tokens []string
		if tok != "" {
			tokens = strings.Fields(tok)
		}
		out = append(out, Measure{
			Tokens:      tokens,
			Open:        opens,
			Close:       closes,
			RepeatCount: repeatCount,
			Volta:       volta,
			Meter:       meter,
		})
	}
	return out
}

// parseLyric turns "Pla[Gm]ceholder words [Bb]here" into segments, mirroring
// v2/lib/song.typ's parse-lyric — minus transposition.
func parseLyric(line string) []LyricSeg {
	var segs []LyricSeg
	rest := line
	var buf strings.Builder
	havePending := false
	pending := ""

	flushSeg := func() {
		if buf.Len() > 0 || havePending {
			segs = append(segs, LyricSeg{HasChord: havePending, Chord: pending, Text: buf.String()})
		}
	}

	for len(rest) > 0 {
		o := strings.IndexByte(rest, '[')
		if o < 0 {
			buf.WriteString(rest)
			rest = ""
			break
		}
		buf.WriteString(rest[:o])
		rest = rest[o+1:]
		c := strings.IndexByte(rest, ']')
		if c < 0 {
			buf.WriteString("[")
			buf.WriteString(rest)
			rest = ""
			break
		}
		ch := rest[:c]
		rest = rest[c+1:]
		flushSeg()
		pending, havePending = ch, true
		buf.Reset()
	}
	flushSeg()

	if len(segs) == 0 {
		segs = append(segs, LyricSeg{})
	}
	return segs
}
