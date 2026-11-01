// Package lilypond converts this app's song.Song model (the same
// .song-DSL variant data internal/song parses everywhere else in the app)
// into LilyPond (.ly) source, for a SECOND, independent PDF export
// pipeline alongside the existing Typst one in internal/pdf. Typst's
// current v2/lib templates render chords as plain bordered bar-grid
// boxes; this pipeline exists specifically to get real engraved
// chords-over-a-staff notation plus sung lyrics via LilyPond's own
// \chordmode / \addlyrics mechanisms. It does not replace or touch the
// Typst pipeline.
//
// IMPORTANT — basis for improvement, not a transcription: the .song
// grammar carries no pitch or rhythm data at all, only chord-over-lyric
// text plus structural bar/beat markers (see internal/song.Measure /
// LyricSeg). This package never attempts real melodic transcription.
// Every notehead it emits is a neutral placeholder pitch (rendered with
// slash noteheads, the same convention real lead sheets use for "comp,
// no specific melody written" passages) — a vehicle to let LilyPond
// position chord symbols and lyrics correctly in time, nothing more.
// Feeding it real pitch/duration data later is explicitly future work.
package lilypond

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"app/internal/chordsym"
	"app/internal/song"
)

// slot is one placeholder-notehead beat: a chord event, a lyric syllable
// (if any), a duration, and whether it's a rest instead of a note.
type slot struct {
	chordAtom string // lilypond \chordmode text, e.g. "g:7", "r" for N.C./rest, "" for a held-over spacer ("s")
	duration  string // lilypond duration suffix (no leading pitch), e.g. "4" or "1*1/3"
	lyric     string // sung syllable, "" if this slot carries no lyric at all
	rest      bool   // true -> melody emits a rest, not a placeholder note
	timeSig   string // "num/den" to emit a \time change right before this slot, "" if unchanged
	mark      string // rehearsal-mark text (section label) to attach right before this slot, "" if none
}

// Render converts s into a complete, standalone .ly source file: a
// \header (title/composer/tempo), one \chordmode stream, one placeholder
// melody \Voice, and (only if the song has any lyric text at all) one
// \Lyrics context \lyricsto that voice. Single voice, single staff,
// single song — no multi-instrument arrangement, no key-signature-aware
// respelling beyond what the chord token text already says.
func Render(s *song.Song) string {
	slots, haveLyrics := buildSlots(s)

	var b strings.Builder
	b.WriteString("\\version \"2.24.0\"\n\n")
	writeHeader(&b, s)

	b.WriteString("songChords = \\chordmode {\n")
	writeChords(&b, slots)
	b.WriteString("}\n\n")

	b.WriteString("songMelody = {\n")
	b.WriteString("  \\override Staff.NoteHead.style = #'slash\n")
	writeMelody(&b, slots)
	b.WriteString("}\n\n")

	if haveLyrics {
		b.WriteString("songLyrics = \\lyricmode {\n")
		writeLyrics(&b, slots)
		b.WriteString("}\n\n")
	}

	b.WriteString("\\score {\n")
	b.WriteString("  <<\n")
	b.WriteString("    \\new ChordNames \\songChords\n")
	b.WriteString("    \\new Staff \\new Voice = \"melody\" \\songMelody\n")
	if haveLyrics {
		b.WriteString("    \\new Lyrics \\lyricsto \"melody\" \\songLyrics\n")
	}
	b.WriteString("  >>\n")
	b.WriteString("  \\layout {}\n")
	b.WriteString("}\n")

	return b.String()
}

func writeHeader(b *strings.Builder, s *song.Song) {
	title := s.Get("title")
	artist := s.Get("artist")
	b.WriteString("\\header {\n")
	b.WriteString("  tagline = ##f\n")
	if title != "" {
		fmt.Fprintf(b, "  title = %q\n", title)
	}
	if artist != "" {
		fmt.Fprintf(b, "  composer = %q\n", artist)
	}
	b.WriteString("}\n\n")
	if tempo := s.Get("tempo"); tempo != "" {
		if bpm, err := strconv.Atoi(strings.TrimSpace(tempo)); err == nil && bpm > 0 {
			fmt.Fprintf(b, "songTempo = { \\tempo 4 = %d }\n\n", bpm)
		}
	}
}

// buildSlots walks every section of s in order and lays out one
// continuous timeline of placeholder beats: grid ("grid:") sections
// contribute one slot per beat-division token (honoring each section's
// own bar/beat grammar — see internal/song.Measure), lyric sections
// contribute one slot per sung word. haveLyrics reports whether any slot
// in the whole song actually carries sung text, so Render can skip
// emitting an empty \Lyrics context.
func buildSlots(s *song.Song) (slots []slot, haveLyrics bool) {
	defaultTime := normalizeTime(s.Get("time"))
	curTime := ""
	var prevBar []barGroup

	for _, sec := range s.Sections {
		if sec.Repeat || (len(sec.Measures) == 0 && len(sec.Lyrics) == 0) {
			// Mirrors v2/lib/song.typ: a (repeat) section (or any section
			// with no grid/lyric body at all) carries no renderable
			// material of its own — just a structural label marker,
			// which there's no slot to attach a \mark to here. A later
			// phase could carry it forward onto the next slot instead.
			continue
		}

		timeSig := defaultTime
		if sec.Time != "" {
			timeSig = normalizeTime(sec.Time)
		}

		pendingMark := sec.Label
		markOnce := func() string {
			m := pendingMark
			pendingMark = ""
			return m
		}
		timeOnce := func() string {
			if timeSig == curTime {
				return ""
			}
			curTime = timeSig
			return timeSig
		}

		for _, m := range sec.Measures {
			groups, bar, k := measureToGroups(m, prevBar)
			prevBar = bar
			for _, g := range groups {
				slots = append(slots, slot{
					chordAtom: g.chordAtom,
					duration:  durationFor(timeSig, k, g.span),
					rest:      g.rest,
					timeSig:   timeOnce(),
					mark:      markOnce(),
				})
			}
		}

		for _, l := range sec.Lyrics {
			for _, seg := range l.Segments {
				words := strings.Fields(seg.Text)
				if len(words) == 0 {
					if !seg.HasChord {
						continue
					}
					words = []string{""} // chord with no trailing lyric text, still needs a slot
				}
				chordAtom := ""
				if seg.HasChord {
					chordAtom = chordToLily(seg.Chord)
				}
				for i, w := range words {
					lyric := w
					if lyric == "" {
						lyric = "_" // lilypond "no syllable printed" placeholder
					} else {
						haveLyrics = true
					}
					atom := ""
					if i == 0 {
						atom = chordAtom
					}
					slots = append(slots, slot{
						chordAtom: atom,
						duration:  "4",
						lyric:     lyric,
						timeSig:   timeOnce(),
						mark:      markOnce(),
					})
				}
			}
		}
	}
	return slots, haveLyrics
}

// barGroup is one post-continuation-merge chord event within a bar, plus
// how many of the bar's original beat-division tokens it spans (a run of
// "." tokens after a chord merges into that chord's span).
type barGroup struct {
	chordAtom string
	rest      bool
	span      int
}

// measureToGroups turns one grid Measure's tokens into barGroups, each
// carrying a span (how many of the bar's k equal beat-divisions it
// covers). "." extends the previous token's span (a held chord); a lone
// "%" token repeats the previous bar's groups verbatim (v2/lib/song.typ's
// own grid convention). Returns the groups actually used this bar (so a
// LATER "%" bar can copy them) and k, the bar's own beat-division count
// — prevBar is only read, never mutated.
func measureToGroups(m song.Measure, prevBar []barGroup) (groups []barGroup, usedBar []barGroup, k int) {
	tokens := m.Tokens
	if len(tokens) == 0 {
		tokens = []string{""}
	}
	if len(tokens) == 1 && tokens[0] == "%" && prevBar != nil {
		total := 0
		for _, g := range prevBar {
			total += g.span
		}
		return prevBar, prevBar, total
	}

	k = len(tokens)
	for _, tok := range tokens {
		if tok == "." && len(groups) > 0 {
			groups[len(groups)-1].span++
			continue
		}
		atom, rest := gridTokenToLily(tok)
		groups = append(groups, barGroup{chordAtom: atom, rest: rest, span: 1})
	}
	return groups, groups, k
}

// gridTokenToLily converts one grid token (a chord symbol, "N.C.", "R"/
// "r", or "" for an empty slot) to a \chordmode atom. "%"/"." are handled
// by the caller before this is reached for anything but the first token
// of a run.
func gridTokenToLily(tok string) (atom string, rest bool) {
	switch tok {
	case "", "%", ".":
		return "s", false // spacer: hold whatever chord was last printed
	case "R", "r":
		return "r", true
	}
	if tok == "N.C." {
		return "r", true
	}
	return chordToLily(tok), false
}

// durationFor returns the lilypond duration suffix for one beat-division
// slot spanning `span` of a bar's `k` equal subdivisions, in a bar whose
// length is timeSig ("num/den"): a whole note scaled by
// num*span/(den*k), reduced. Scaled-duration syntax (base "1" multiplied
// by an arbitrary reduced fraction) handles any subdivision count
// generally, not just powers of two — e.g. 3 equal beats in a 4/4 bar.
func durationFor(timeSig string, k, span int) string {
	num, den := splitTime(timeSig)
	if k <= 0 {
		k = 1
	}
	n, d := num*span, den*k
	if g := gcd(n, d); g > 1 {
		n, d = n/g, d/g
	}
	return fmt.Sprintf("1*%d/%d", n, d)
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

var timeRE = regexp.MustCompile(`^(\d+)\s*/\s*(\d+)$`)

// normalizeTime validates a "time:" field value, defaulting to "4/4" for
// anything empty or unparseable.
func normalizeTime(v string) string {
	if timeRE.MatchString(strings.TrimSpace(v)) {
		return strings.TrimSpace(v)
	}
	return "4/4"
}

func splitTime(v string) (num, den int) {
	m := timeRE.FindStringSubmatch(v)
	if m == nil {
		return 4, 4
	}
	num, _ = strconv.Atoi(m[1])
	den, _ = strconv.Atoi(m[2])
	if num <= 0 {
		num = 4
	}
	if den <= 0 {
		den = 4
	}
	return num, den
}

func writeChords(b *strings.Builder, slots []slot) {
	for _, s := range slots {
		writePrefix(b, s)
		atom := s.chordAtom
		if atom == "" {
			atom = "s"
		}
		fmt.Fprintf(b, "%s ", insertDuration(atom, s.duration))
	}
	b.WriteString("\n")
}

// insertDuration splices a duration suffix into a \chordmode atom at the
// position lilypond actually requires it: right after the root pitch,
// before any ":quality" or "/bass" suffix — "g:m7" + "4" must become
// "g4:m7", not the invalid "g:m74". Atoms with no suffix at all ("s",
// "r", a bare root) just get it appended.
func insertDuration(atom, duration string) string {
	if i := strings.IndexAny(atom, ":/"); i >= 0 {
		return atom[:i] + duration + atom[i:]
	}
	return atom + duration
}

func writeMelody(b *strings.Builder, slots []slot) {
	for _, s := range slots {
		writePrefix(b, s)
		pitch := "c'"
		if s.rest {
			pitch = "r"
		}
		fmt.Fprintf(b, "%s%s ", pitch, s.duration)
	}
	b.WriteString("\n")
}

func writeLyrics(b *strings.Builder, slots []slot) {
	for _, s := range slots {
		if s.lyric == "" {
			b.WriteString("\\skip 1 ")
			continue
		}
		fmt.Fprintf(b, "%q ", s.lyric)
	}
	b.WriteString("\n")
}

func writePrefix(b *strings.Builder, s slot) {
	if s.timeSig != "" {
		fmt.Fprintf(b, "\\time %s\n", s.timeSig)
	}
	if s.mark != "" {
		fmt.Fprintf(b, "\\mark \\markup { \\bold %q }\n", s.mark)
	}
}

// --- chord symbol -> lilypond chordmode text -----------------------------

var (
	halfDimRE    = regexp.MustCompile(`(?i)^(?:mi|min|m|-)(\d*)[b-]5$`)
	halfDimLitRE = regexp.MustCompile(`^(?:ø|o/|0)(\d*)$`)
	dimRE        = regexp.MustCompile(`(?i)^dim(\d*)$`)
	dimLetRE     = regexp.MustCompile(`^o(\d*)$`)
	augRE        = regexp.MustCompile(`(?i)^(?:aug|\+)(\d*)$`)
	minorRE      = regexp.MustCompile(`(?i)^(?:mi|min|m|-)(\d+)?$`)
)

// chordToLily converts one chord-token string ("Gm7", "Bb", "F#:11",
// "E7b5-/G", "N.C.") into a \chordmode atom ("g:m7", "bes", ...),
// borrowing internal/chordsym's root/quality/bass split (the same one
// the HTML chart view uses) rather than re-parsing chord text a third
// time. Anything chordsym can't find a root for (N.C., "%", free text)
// becomes a rest. Quality mapping covers the common cases this app's
// charts actually use; anything else falls back to a best-effort
// lowercase pass-through — intentionally not exhaustive, see the package
// doc comment.
func chordToLily(tok string) string {
	tok = strings.TrimSpace(tok)
	p := chordsym.Split(tok)
	if p.Root == "" {
		return "r"
	}
	out := rootToLily(p.Root)
	if q := qualityToLily(p.Quality); q != "" {
		out += q
	}
	if p.Bass != "" {
		out += "/" + rootToLily(p.Bass)
	}
	return out
}

// rootToLily converts a chart root ("Bb", "F#", "C") to lilypond's
// default (Dutch) pitch-name spelling: lowercase letter plus "is"
// (sharp) or "es" (flat) — uniform for all seven letters, including the
// less common "ces"/"fis"/"eis" spellings, all valid lilypond input.
func rootToLily(root string) string {
	if root == "" {
		return "c"
	}
	letter := strings.ToLower(string(root[0]))
	if len(root) > 1 {
		switch root[1] {
		case '#':
			return letter + "is"
		case 'b':
			return letter + "es"
		}
	}
	return letter
}

// qualityToLily maps a chart's quality text (everything after the root,
// before any "/bass") to a \chordmode modifier string, INCLUDING its
// leading ":" (so "" means "no modifier at all", a plain major chord).
func qualityToLily(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}

	switch {
	case halfDimRE.MatchString(q), halfDimLitRE.MatchString(q):
		return ":m7.5-"
	case dimRE.MatchString(q), dimLetRE.MatchString(q):
		if strings.HasSuffix(q, "7") {
			return ":dim7"
		}
		return ":dim"
	case augRE.MatchString(q):
		return ":aug"
	case minorRE.MatchString(q):
		m := minorRE.FindStringSubmatch(q)
		if m[1] != "" {
			return ":m" + m[1]
		}
		return ":m"
	}

	switch strings.ToLower(q) {
	case "maj7", "ma7", "M7":
		return ":maj7"
	case "maj9":
		return ":maj9"
	case "6":
		return ":6"
	case "69", "6/9":
		return ":6.9"
	case "sus2":
		return ":sus2"
	case "sus4", "sus":
		return ":sus4"
	case "7b5", "7-5":
		return ":7.5-"
	case "7#5", "7+5":
		return ":7.5+"
	case "7b9", "7-9":
		return ":7.9-"
	case "7#9", "7+9":
		return ":7.9+"
	}

	// Best-effort fallback: numeric/plain extensions ("7", "9", "11",
	// "13", "add9", ...) pass through lowercased, anything lilypond
	// chordmode won't recognize simply prints oddly rather than failing
	// the whole compile — acceptable for a "basis for improvement" v1.
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return -1
	}, q)
	if cleaned == "" {
		return ""
	}
	return ":" + cleaned
}
