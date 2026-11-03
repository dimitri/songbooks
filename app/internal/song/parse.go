package song

import (
	"os"
	"strconv"
	"strings"
)

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
	score   string
	times   int
	bars    []int
	gridRaw strings.Builder
	lines   []string
	notes   []string
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
			Score:       cur.score,
			Times:       cur.times,
			BarsPerLine: cur.bars,
			Lyrics:      make([]LyricLine, 0, len(cur.lines)),
			Notes:       cur.notes,
		}
		if spec := strings.TrimSpace(cur.gridRaw.String()); spec != "" {
			sec.Measures = parseGrid(spec)
		}
		for _, l := range cur.lines {
			sec.Lyrics = append(sec.Lyrics, LyricLine{Segments: parseLyric(l)})
		}
		song.Sections = append(song.Sections, sec)
	}

	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && (strings.HasSuffix(line, "]") || strings.HasSuffix(line, "] (repeat)")) {
			flush()
			repeat := strings.HasSuffix(line, "(repeat)")
			var label string
			if repeat {
				label = line[1:strings.Index(line, "]")]
			} else {
				label = line[1 : len(line)-1]
			}
			cur = &rawSection{label: label, repeat: repeat}
			inHead = false
			continue
		}

		if inHead {
			if line == "" {
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

		if cur == nil || line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "time:"):
			cur.time = strings.TrimSpace(line[5:])
		case strings.HasPrefix(line, "score:"):
			cur.score = strings.TrimSpace(line[6:])
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
		case strings.HasPrefix(line, "|") && len(cur.lines) == 0:
			// Grid lines always precede lyric lines within a section; once
			// lyric text has started, a "|"-leading line is lyric content.
			cur.gridRaw.WriteString(" " + line)
		default:
			cur.lines = append(cur.lines, line)
		}
	}
	flush()

	return song
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
		if opens {
			tok = strings.TrimSpace(tok[1:])
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
			Tokens: tokens,
			Open:   opens,
			Close:  closes,
			Volta:  volta,
			Meter:  meter,
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
