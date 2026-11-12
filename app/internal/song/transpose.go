package song

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"app/internal/chordsym"
)

// ErrUnknownKey is returned by TransposeText for a key it can't read.
var ErrUnknownKey = errors.New("unknown key")

// ErrModeChange is returned by TransposeText when asked to move a major
// song to a minor key or back: that's rewriting the song, not transposing.
var ErrModeChange = errors.New("a song in a major key can only move to another major key, and minor to minor")

// TransposeOptions says where a song goes. From is the key the text is
// written in ("" = its "key:" header, or a guess from its first chord when
// it has none). To is the new key; when it's "", Semitones moves From.
type TransposeOptions struct {
	From      string
	To        string
	Semitones int
}

// TransposeResult is the rewritten text and what was done to it.
type TransposeResult struct {
	Text      string
	From      string
	To        string
	Semitones int // -5..+6, the shortest way from From to To
	// Kept counts the diagrams that were the song's own and now stay
	// registered under From; Restored counts "chord[To]:" diagrams that
	// became the song's own set again.
	Kept     int
	Restored int
}

// TransposeText rewrites a song's source into another key, line by line,
// so everything that isn't a chord keeps its exact bytes (comments,
// spacing, score[...] voices, notes). What changes:
//   - every chord in grid: lines and bar lines (durations, ties, repeat
//     and volta marks around it untouched; N.C., %, ., R left alone);
//   - every [Chord] in lyric lines that is a real chord ([Bridge] isn't);
//   - slash basses, spelled as they read in the new key;
//   - the "key:" header (added after title/artist when missing);
//   - diagrams: plain "chord:" lines belonged to the old key, so they
//     become "chord[Old]:"; "chord[New]:" lines become the song's own
//     plain "chord:" lines again.
//
// Line classification follows Parse exactly (see TestTransposeMatchesParse).
func TransposeText(src string, opt TransposeOptions) (*TransposeResult, error) {
	parsed := Parse(src)
	headerKey := parsed.Get("key")
	fromName := opt.From
	if fromName == "" {
		fromName = headerKey
	}
	if fromName == "" {
		fromName, _ = guessKey(parsed)
	}
	from, ok := chordsym.ParseKey(fromName)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKey, fromName)
	}
	flatsPref := parsed.Get("flats") == "yes"

	var to chordsym.Key
	if opt.To != "" {
		if to, ok = chordsym.ParseKey(opt.To); !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownKey, opt.To)
		}
		if to.Minor != from.Minor {
			return nil, ErrModeChange
		}
		// Keep the author's way of writing the mode ("A minor" -> "G minor").
		to.Suffix = from.Suffix
	} else {
		to = from.Transpose(opt.Semitones, flatsPref)
	}
	n := ((to.PitchClass()-from.PitchClass())%12 + 12) % 12
	if n > 6 {
		n -= 12
	}

	// Plain diagrams belong to the "key:" header's key; only a song with
	// no header falls back to the key the caller says it's written in.
	oldDiagramKey := headerKey
	if oldDiagramKey == "" {
		oldDiagramKey = from.String()
	}

	tr := transposer{n: n, to: to, flats: flatsPref}
	res := &TransposeResult{From: from.String(), To: to.String(), Semitones: n}

	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines)+1)
	inHead := true
	sawKey := false
	keySlot := 0 // where a missing "key:" line goes: after title/artist
	lyricsStarted := false
	inScore := false
	scoreDepth := 0

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			out = append(out, raw)
			continue
		}
		if sectionHeaderRE.MatchString(line) {
			inHead, lyricsStarted, inScore = false, false, false
			out = append(out, raw)
			continue
		}
		if inHead {
			out = append(out, tr.headerLine(raw, line, oldDiagramKey, res, &sawKey))
			if strings.HasPrefix(line, "title:") || strings.HasPrefix(line, "artist:") {
				keySlot = len(out)
			}
			continue
		}

		if inScore {
			if scoreDepth > 0 || scoreContinues(line) {
				scoreDepth = max(0, scoreDepth+strings.Count(line, "{")-strings.Count(line, "}"))
				out = append(out, raw)
				continue
			}
			inScore = false
		}
		if line == "" {
			out = append(out, raw)
			continue
		}

		switch {
		case strings.HasPrefix(line, "score["):
			if _, val, ok := parseScoreHeader(line); ok {
				inScore = true
				scoreDepth = max(0, strings.Count(val, "{")-strings.Count(val, "}"))
			}
			out = append(out, raw)
		case strings.HasPrefix(line, "time:"), strings.HasPrefix(line, "bars:"),
			strings.HasPrefix(line, "times:"), strings.HasPrefix(line, "note:"),
			strings.HasPrefix(line, "score:"):
			out = append(out, raw)
		case strings.HasPrefix(line, "grid:"):
			i := strings.Index(raw, "grid:") + len("grid:")
			out = append(out, raw[:i]+tr.grid(raw[i:]))
		case strings.HasPrefix(line, "|") && !lyricsStarted:
			out = append(out, tr.grid(raw))
		default:
			lyricsStarted = true
			out = append(out, tr.lyric(raw))
		}
	}

	if !sawKey {
		out = append(out[:keySlot], append([]string{"key: " + to.String()}, out[keySlot:]...)...)
	}
	res.Text = strings.Join(out, "\n")
	return res, nil
}

type transposer struct {
	n     int
	to    chordsym.Key
	flats bool
}

// headerLine rewrites "key:" and diagram lines; every other header line
// passes through.
func (tr transposer) headerLine(raw, line, oldDiagramKey string, res *TransposeResult, sawKey *bool) string {
	indent := raw[:strings.Index(raw, line)]
	if key, rest, ok := splitChordLine(line); ok {
		switch {
		case key == "":
			res.Kept++
			return indent + "chord[" + oldDiagramKey + "]:" + rest
		case chordsym.SameKey(key, tr.to.String()):
			res.Restored++
			return indent + "chord:" + rest
		}
		return raw
	}
	if i := strings.Index(line, ":"); i >= 0 && strings.TrimSpace(line[:i]) == "key" {
		*sawKey = true
		return indent + "key: " + tr.to.String()
	}
	return raw
}

var gridTokenRE = regexp.MustCompile(`[^\s|]+`)

// grid transposes every chord token of a bar line, leaving the bar lines,
// spacing and non-chord tokens exactly as written.
func (tr transposer) grid(s string) string {
	return gridTokenRE.ReplaceAllStringFunc(s, tr.token)
}

var lyricChordRE = regexp.MustCompile(`\[[^\[\]]*\]`)

func (tr transposer) lyric(s string) string {
	return lyricChordRE.ReplaceAllStringFunc(s, func(m string) string {
		return "[" + tr.token(m[1:len(m)-1]) + "]"
	})
}

// token transposes the chord inside one grid token or lyric bracket.
func (tr transposer) token(t string) string {
	pre, core, post := chordCore(t)
	out, ok := chordsym.TransposeInKey(core, tr.n, tr.to, tr.flats)
	if !ok {
		return t
	}
	return pre + out + post
}

// chordCore splits a grid token into what comes before the chord (a
// repeat bracket's ":" and count, a tie), the chord itself, and what
// comes after it (a ":duration" tag, a closing ":", a tie).
func chordCore(t string) (pre, core, post string) {
	i := 0
	for i < len(t) && (t[i] == ':' || t[i] == '~' || (t[i] >= '0' && t[i] <= '9')) {
		i++
	}
	j := i
	for j < len(t) && t[j] != ':' && t[j] != '~' {
		j++
	}
	return t[:i], t[i:j], t[j:]
}

// UsedChords lists the distinct chord symbols a song plays, in order of
// first appearance (grids, then lyrics, section by section), durations
// and ties stripped. N.C., rests and repeat marks aren't chords.
func UsedChords(s *Song) []string {
	var out []string
	seen := map[string]bool{}
	add := func(t string) {
		_, c, _ := chordCore(t)
		if chordsym.IsChord(c) && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, sec := range s.Sections {
		for _, m := range sec.Measures {
			for _, t := range m.Tokens {
				add(t)
			}
		}
		for _, l := range sec.Lyrics {
			for _, seg := range l.Segments {
				if seg.HasChord {
					add(seg.Chord)
				}
			}
		}
	}
	return out
}

// guessKey names the key a song without a "key:" header is probably in:
// its first chord, minor when that chord is.
func guessKey(s *Song) (string, bool) {
	used := UsedChords(s)
	if len(used) == 0 {
		return "", false
	}
	p := chordsym.Split(used[0])
	if chordsym.DecomposeQuality(p.Quality).Marker == chordsym.MarkerMinor {
		return p.Root + "m", true
	}
	return p.Root, true
}

// Analysis is what the editor needs to offer a transpose and to ask for
// missing diagrams.
type Analysis struct {
	Key        string // the "key:" header, or a guess when there's none
	KeyGuessed bool
	Chords     []string // UsedChords
	// Missing are the used chords with no diagram in the song's own set.
	Missing []string
	// DiagramKeys are the keys that have diagrams: the song's own key
	// first when it has any, then the other keys in source order.
	DiagramKeys []string
}

// Analyze reads a song's source for the editor's transpose and diagram
// tools.
func Analyze(src string) Analysis {
	s := Parse(src)
	a := Analysis{Key: s.Get("key"), Chords: UsedChords(s)}
	if a.Key == "" {
		a.Key, a.KeyGuessed = guessKey(s)
	}
	for _, c := range a.Chords {
		if !hasDiagram(s.Chords, c) {
			a.Missing = append(a.Missing, c)
		}
	}
	if len(s.Chords) > 0 {
		a.DiagramKeys = append(a.DiagramKeys, a.Key)
	}
	for _, set := range s.DiagramSets() {
		a.DiagramKeys = append(a.DiagramKeys, set.Key)
	}
	return a
}

func hasDiagram(refs []ChordRef, chord string) bool {
	for _, r := range refs {
		if chordsym.SameChord(r.Name, chord) {
			return true
		}
	}
	return false
}
