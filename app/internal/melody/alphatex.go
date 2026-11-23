package melody

import (
	"fmt"
	"slices"
	"strings"

	"app/internal/chordsym"
)

// Score is a sheet ready to write: its notes, what each sings, and how
// it's to be read (key, tempo, sections).
type Score struct {
	Sheet  Sheet
	Lyrics []NoteLyric // per Sheet.Notes; nil: no lyrics
	// LowLines: the lines placed unsurely (by index in the lines laid).
	LowLines map[int]bool
	// Transpose: semitones from the recording's key to the page's.
	Transpose int
	Key       Key
	Tempo     float64
	Sections  map[int]string // the label of the section starting at a played bar
	Title     string
}

// Key is a key signature: its sharps (+) or flats (-), and its mode.
type Key struct {
	Fifths int
	Minor  bool
}

var majorFifths = map[int]int{0: 0, 7: 1, 2: 2, 9: 3, 4: 4, 11: 5, 6: 6, 1: -5, 8: -4, 3: -3, 10: -2, 5: -1}

// KeyOf reads a chart's "key:" ("F#m", "Bb") moved by transpose
// semitones; ok false when there's none to read. flats: F♯/G♭ is
// written G♭ (six flats) rather than F♯.
func KeyOf(s string, transpose int, flats bool) (Key, bool) {
	k, ok := chordsym.ParseKey(s)
	if !ok {
		return Key{}, false
	}
	root, minor := k.Root, k.Minor
	pc := k.PitchClass()
	if pc < 0 {
		return Key{}, false
	}
	pc = ((pc+transpose)%12 + 12) % 12
	major := pc
	if minor {
		major = (pc + 3) % 12
	}
	f := majorFifths[major]
	if f == 6 && (flats || strings.Contains(root, "b")) {
		f = -6
	}
	return Key{Fifths: f, Minor: minor}, true
}

var majorNames = []string{"cb", "gb", "db", "ab", "eb", "bb", "f", "c", "g", "d", "a", "e", "b", "f#", "c#"}
var minorNames = []string{"abminor", "ebminor", "bbminor", "fminor", "cminor", "gminor", "dminor", "aminor", "eminor", "bminor", "f#minor", "c#minor", "g#minor", "d#minor", "a#minor"}

func (k Key) alphaTex() string {
	i := max(-7, min(7, k.Fifths)) + 7
	if k.Minor {
		return minorNames[i]
	}
	return majorNames[i]
}

var sharpNames = []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
var flatNames = []string{"C", "Db", "D", "Eb", "E", "F", "Gb", "G", "Ab", "A", "Bb", "B"}

// spell is a MIDI note as a step, an alteration (-1, 0, 1) and an
// octave (C4 = 60), sharps or flats as the key has them.
func (k Key) spell(midi int) (step string, alter, octave int) {
	names := sharpNames
	if k.Fifths < 0 {
		names = flatNames
	}
	n := names[((midi%12)+12)%12]
	step = n[:1]
	if len(n) > 1 {
		alter = 1
		if n[1] == 'b' {
			alter = -1
		}
	}
	return step, alter, midi/12 - 1
}

// BeatMark is a beat the page draws lighter: its bar and its index in
// the bar (rests counted), an unsure note, a note of an unsure line.
type BeatMark struct {
	Bar      int  `json:"bar"`
	Beat     int  `json:"beat"`
	LowNote  bool `json:"lowNote,omitempty"`
	LowLyric bool `json:"lowLyric,omitempty"`
}

var durations = map[int]struct {
	v   int
	dot bool
}{16: {1, false}, 12: {2, true}, 8: {2, false}, 6: {4, true}, 4: {4, false}, 3: {8, true}, 2: {8, false}, 1: {16, false}}

// lowClef: a voice sung this low is written on the treble clef an octave
// down (a man's voice), not under a forest of ledger lines.
const lowClef = 60

// median MIDI note of the sheet, transposed.
func (s Score) medianNote() int {
	if len(s.Sheet.Notes) == 0 {
		return 67
	}
	m := make([]int, len(s.Sheet.Notes))
	for i, n := range s.Sheet.Notes {
		m[i] = n.Midi
	}
	slices.Sort(m)
	return m[len(m)/2] + s.Transpose
}

// AlphaTex is the sheet as alphaTex (one voice on a treble staff, the
// lyrics under it), and the beats the page draws lighter.
func (s Score) AlphaTex() (string, []BeatMark) {
	var b strings.Builder
	if s.Title != "" {
		fmt.Fprintf(&b, "\\title (%s)\n", texString(s.Title))
	}
	if s.Tempo > 0 {
		fmt.Fprintf(&b, "\\tempo (%d)\n", int(s.Tempo+0.5))
	}
	b.WriteString("\\track (\"Voice\")\n\\staff {score}\n\\tuning (piano)\n\\instrument (53)\n")
	if s.Lyrics != nil {
		fmt.Fprintf(&b, "\\lyrics (%s)\n", texString(s.lyricText()))
	}
	low := s.medianNote() < lowClef
	var marks []BeatMark
	var meter Meter
	for bi, bar := range s.Sheet.Bars {
		if bi > 0 {
			b.WriteString(" |\n")
		}
		if bi == 0 {
			fmt.Fprintf(&b, "\\clef (G2) ")
			if low {
				b.WriteString("\\ottava (8vb) ")
			}
			fmt.Fprintf(&b, "\\ks (%s) ", s.Key.alphaTex())
		}
		if bar.Meter != meter {
			fmt.Fprintf(&b, "\\ts (%d %d) ", bar.Meter.Num, bar.Meter.Den)
			meter = bar.Meter
		}
		if l, ok := s.Sections[bar.Played]; ok && l != "" {
			fmt.Fprintf(&b, "\\section (%s) ", texString(l))
		}
		for ei, ev := range bar.Events {
			if ei > 0 {
				b.WriteByte(' ')
			}
			d := durations[ev.Len]
			if ev.Midi < 0 {
				fmt.Fprintf(&b, "r.%d", d.v)
			} else {
				step, alter, oct := s.Key.spell(ev.Midi + s.Transpose)
				acc := ""
				if alter > 0 {
					acc = "#"
				} else if alter < 0 {
					acc = "b"
				}
				b.WriteString(step + acc + fmt.Sprint(oct))
				if ev.TieIn {
					b.WriteString("{t}")
				}
				fmt.Fprintf(&b, ".%d", d.v)
				n := s.Sheet.Notes[ev.Note]
				m := BeatMark{Bar: bi, Beat: ei, LowNote: n.Low}
				if s.Lyrics != nil {
					if l := s.Lyrics[ev.Note].Line; l >= 0 && s.LowLines[l] {
						m.LowLyric = true
					}
				}
				if m.LowNote || m.LowLyric {
					marks = append(marks, m)
				}
			}
			if d.dot {
				b.WriteString("{d}")
			}
		}
	}
	b.WriteString("\n")
	return b.String(), marks
}

// lyricText is the lyrics as alphaTex lays them: a token per note beat
// (rests take none), "_" for a beat that sings no new syllable (a tied
// beat, a melisma, a note without words), a word's syllables ending in
// "-", two syllables sung on one note joined by "+".
func (s Score) lyricText() string {
	var toks []string
	for _, bar := range s.Sheet.Bars {
		for _, ev := range bar.Events {
			if ev.Midi < 0 {
				continue
			}
			l := s.Lyrics[ev.Note]
			if ev.TieIn || len(l.Syls) == 0 {
				toks = append(toks, "_")
				continue
			}
			toks = append(toks, sylToken(l.Syls))
		}
	}
	// Trailing beats with nothing to sing need no token.
	for len(toks) > 0 && toks[len(toks)-1] == "_" {
		toks = toks[:len(toks)-1]
	}
	return strings.Join(toks, " ")
}

// sylToken writes syllables sung on one note: a word's run joined as
// it's spelled, words joined by "+"; "-" when the word goes on.
func sylToken(syls []Syl) string {
	var b strings.Builder
	for i, sy := range syls {
		if i > 0 && sy.WordStart {
			b.WriteByte('+')
		}
		b.WriteString(clean(sy.Text))
	}
	if !syls[len(syls)-1].WordEnd {
		b.WriteByte('-')
	}
	out := b.String()
	if out == "" || out == "-" {
		return "_"
	}
	return out
}

// clean keeps a syllable's text out of alphaTex's way: no quotes,
// backslashes, spaces or the characters its lyrics read.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '"', '\\', '+', '_', '-', ' ', '\t', '\n', '{', '}', '|':
			return -1
		}
		return r
	}, s)
}

func texString(s string) string {
	s = strings.NewReplacer(`\`, "", `"`, "'", "\n", " ").Replace(s)
	return `"` + s + `"`
}
