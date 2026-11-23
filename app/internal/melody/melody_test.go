package melody

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"app/internal/audioanalysis"
)

// A steady 120 bpm 4/4 grid: 2 s a bar from t0.
func steady(t0 float64, bars int) Grid {
	g := make(Grid, bars+1)
	for b := range g {
		g[b] = t0 + 2*float64(b)
	}
	return g
}

func four(int) Meter { return Meter{4, 4} }

// note at beat position (in quarter notes from bar 0 of a 2 s/bar grid
// starting at t0) for beats.
func noteAt(t0, beat, beats float64, midi float64) audioanalysis.SungNote {
	return audioanalysis.SungNote{Start: t0 + beat*0.5, End: t0 + (beat+beats)*0.5, Pitch: midi, Conf: 0.9}
}

func TestGridFromPointsAndMarkers(t *testing.T) {
	g := FromPoints([]Point{{0, 1}, {4, 9}}, 20)
	if g[2] != 5 || g.Bars() < 9 {
		t.Fatalf("points: %v", g)
	}
	if b := g.BarAt(6); math.Abs(b-2.5) > 1e-9 {
		t.Errorf("BarAt(6) = %v, want 2.5", b)
	}
	if tt := g.TimeAt(2.5); math.Abs(tt-6) > 1e-9 {
		t.Errorf("TimeAt(2.5) = %v, want 6", tt)
	}
	// Two sections: 4 bars from 10 s to 18 s (2 s a bar), the next from
	// 18 s; the chart says 1.5 s a bar outside them.
	g = FromMarkers([]Mark{{2, 10}, {6, 18}}, 1.5, 40)
	if g[0] != 7 || g[4] != 14 || g[6] != 18 || g[7] != 19.5 {
		t.Fatalf("markers: %v", g[:8])
	}
}

func TestGridLead(t *testing.T) {
	// lead: 2, the first section at 5 s, 2 s a bar: the lead from 1 s.
	g := PlaceLead(Grid{0, 1, 5, 7, 9}, 2, 2)
	if g[0] != 1 || g[1] != 3 || g[2] != 5 {
		t.Fatalf("lead: %v", g)
	}
	// Not enough time before it: squeezed from 0:00.
	g = PlaceLead(Grid{0, 1, 2, 4, 6}, 2, 2)
	if g[0] != 0 || g[1] != 1 {
		t.Fatalf("squeezed lead: %v", g)
	}
	if p := FirstPace(Grid{1, 3, 5, 7, 9}, 2, 2); p != 2 {
		t.Errorf("first pace %v", p)
	}
}

func eventsString(b Bar) string {
	var s []string
	for _, e := range b.Events {
		x := "r"
		if e.Midi >= 0 {
			x = fmt.Sprint(e.Midi)
		}
		if e.TieIn {
			x = "~" + x
		}
		s = append(s, fmt.Sprintf("%s/%d", x, e.Len))
	}
	return strings.Join(s, " ")
}

func TestQuantizeEighths(t *testing.T) {
	g := steady(1, 4)
	// Bar 0: a quarter, two eighths sung a little early and late, a half;
	// bar 1: a rest, then a note held over the bar line into bar 2.
	notes := []audioanalysis.SungNote{
		noteAt(1, 0.02, 0.95, 60),
		noteAt(1, 0.97, 0.45, 62),
		noteAt(1, 1.55, 0.4, 64),
		noteAt(1, 2, 1.9, 65),
		noteAt(1, 6, 4, 67),
	}
	sh := Quantize(notes, g, four, Options{FirstBar: -1, LastBar: -1})
	if len(sh.Bars) != 3 {
		t.Fatalf("%d bars", len(sh.Bars))
	}
	want := []string{"60/4 62/2 64/2 65/8", "r/8 67/8", "~67/8 r/8"}
	for i, w := range want {
		if got := eventsString(sh.Bars[i]); got != w {
			t.Errorf("bar %d: %s, want %s", i, got, w)
		}
	}
	if sh.Bars[0].Fine {
		t.Error("eighths written in sixteenths")
	}
}

func TestQuantizeSixteenthsWhereDense(t *testing.T) {
	g := steady(0, 2)
	// Four sixteenths on beat 1: eighths would put two on one place.
	var notes []audioanalysis.SungNote
	for k := range 4 {
		notes = append(notes, noteAt(0, float64(k)*0.25, 0.24, float64(60+k)))
	}
	notes = append(notes, noteAt(0, 4, 2, 72)) // bar 1 stays in eighths
	sh := Quantize(notes, g, four, Options{FirstBar: -1, LastBar: -1})
	if !sh.Bars[0].Fine || sh.Bars[1].Fine {
		t.Fatalf("fine: %v %v", sh.Bars[0].Fine, sh.Bars[1].Fine)
	}
	if got := eventsString(sh.Bars[0]); got != "60/1 61/1 62/1 63/1 r/4 r/8" {
		t.Errorf("bar 0: %s", got)
	}
}

func TestQuantizeDropsBeforeGridAndRepeats(t *testing.T) {
	// Played bars 0..3 (a "|: … :|" two-bar phrase played twice): the
	// second pass's notes land in bars 2 and 3, not on the first pass.
	g := steady(10, 4)
	notes := []audioanalysis.SungNote{
		{Start: 5, End: 6, Pitch: 60, Conf: 0.9}, // before bar 0: dropped
		noteAt(10, 0, 2, 62),
		noteAt(10, 8, 2, 64),
	}
	sh := Quantize(notes, g, four, Options{FirstBar: 0, LastBar: 3})
	if sh.Dropped != 1 || len(sh.Notes) != 2 {
		t.Fatalf("dropped %d, placed %d", sh.Dropped, len(sh.Notes))
	}
	if sh.Notes[1].Bar != 2 || eventsString(sh.Bars[2]) != "64/8 r/8" || eventsString(sh.Bars[1]) != "r/16" {
		t.Errorf("second pass: bar %d, %q, %q", sh.Notes[1].Bar, eventsString(sh.Bars[2]), eventsString(sh.Bars[1]))
	}
}

func TestQuantizeMeters(t *testing.T) {
	g := steady(0, 2)
	meter := func(b int) Meter {
		if b == 0 {
			return Meter{3, 4}
		}
		return Meter{6, 8}
	}
	// A whole bar of 3/4 (2 s): one dotted half; 6/8: two dotted quarters.
	notes := []audioanalysis.SungNote{{Start: 0, End: 2, Pitch: 60, Conf: 1}, {Start: 2, End: 3, Pitch: 62, Conf: 1}, {Start: 3, End: 4, Pitch: 64, Conf: 1}}
	sh := Quantize(notes, g, meter, Options{FirstBar: -1, LastBar: -1})
	if got := eventsString(sh.Bars[0]); got != "60/12" {
		t.Errorf("3/4: %s", got)
	}
	if got := eventsString(sh.Bars[1]); got != "62/6 64/6" {
		t.Errorf("6/8: %s", got)
	}
}

func spans(notes []audioanalysis.SungNote) []Span {
	out := make([]Span, len(notes))
	for i, n := range notes {
		out[i] = Span{n.Start, n.End}
	}
	return out
}

// phraseNotes makes count notes from t, 0.3 s each.
func phraseNotes(t float64, count int) []audioanalysis.SungNote {
	var out []audioanalysis.SungNote
	for k := range count {
		out = append(out, audioanalysis.SungNote{Start: t + 0.3*float64(k), End: t + 0.3*float64(k) + 0.28, Pitch: 64, Conf: 0.9})
	}
	return out
}

func line(key, text string) Line {
	return Line{Key: key, Text: text, Syls: SplitLine(text, English), From: math.NaN(), To: math.NaN()}
}

func TestAlignLinesEqualCounts(t *testing.T) {
	var notes []audioanalysis.SungNote
	notes = append(notes, phraseNotes(0, 6)...)  // "Sing a little song to me" 6 syllables (sing a litt le song to me: 7)
	notes = append(notes, phraseNotes(4, 5)...)  // 5
	notes = append(notes, phraseNotes(8, 7)...)  // 7
	lines := []Line{line("0:0", "walking in the rain"), line("0:1", "under the sky"), line("0:2", "nobody knows my name")}
	sp := spans(notes)
	ph := PhrasesFromNotes(sp, 0.35)
	if len(ph) != 3 {
		t.Fatalf("%d phrases", len(ph))
	}
	fits, lyr := AlignLines(lines, ph, sp, nil)
	for i, f := range fits {
		if !f.Matched || f.First != ph[i].First {
			t.Errorf("line %d: %+v, want phrase %d", i, f, i)
		}
	}
	// "wal-king in the rain" (5) on 6 notes: the last note holds "rain".
	if lyr[0].Syls[0].Text != "wal" || !lyr[5].Hold || lyr[4].Syls[0].Text != "rain" {
		t.Errorf("spread: %+v", lyr[:6])
	}
}

func TestAlignLinesExtraPhrase(t *testing.T) {
	// An ad-lib (two notes) between two lines: left out.
	var notes []audioanalysis.SungNote
	notes = append(notes, phraseNotes(0, 5)...)
	notes = append(notes, phraseNotes(3, 2)...)
	notes = append(notes, phraseNotes(6, 6)...)
	lines := []Line{line("0:0", "hold me now darling"), line("0:1", "never let me go away")}
	sp := spans(notes)
	fits, _ := AlignLines(lines, PhrasesFromNotes(sp, 0.35), sp, nil)
	if fits[0].First != 0 || fits[1].First != 7 {
		t.Fatalf("fits %+v", fits)
	}
}

func TestAlignLinesMissingPhrase(t *testing.T) {
	// Three lines, two phrases: the middle line, whose section the sync
	// puts where no phrase is, finds none.
	var notes []audioanalysis.SungNote
	notes = append(notes, phraseNotes(0, 5)...)
	notes = append(notes, phraseNotes(10, 5)...)
	lines := []Line{line("0:0", "one two three four five"), line("1:0", "six seven eight nine"), line("2:0", "ten eleven twelve")}
	lines[0].From, lines[0].To = 0, 4
	lines[1].From, lines[1].To = 4, 9
	lines[2].From, lines[2].To = 9, 14
	sp := spans(notes)
	fits, _ := AlignLines(lines, PhrasesFromNotes(sp, 0.35), sp, nil)
	if !fits[0].Matched || fits[1].Matched || !fits[2].Matched || fits[2].First != 5 {
		t.Fatalf("fits %+v", fits)
	}
}

func TestAlignLinesShift(t *testing.T) {
	notes := phraseNotes(0, 6)
	sp := spans(notes)
	lines := []Line{line("0:0", "la la la")}
	fits, lyr := AlignLines(lines, PhrasesFromNotes(sp, 0.35), sp, map[string]int{"0:0": 2})
	if fits[0].First != 2 || fits[0].Shift != 2 || len(lyr[0].Syls) != 0 || lyr[2].Syls[0].Text != "la" {
		t.Fatalf("shift: %+v %+v", fits[0], lyr[:3])
	}
}

func TestAlignMoreSyllablesThanNotes(t *testing.T) {
	notes := phraseNotes(0, 3)
	sp := spans(notes)
	_, lyr := AlignLines([]Line{line("0:0", "hello beautiful")}, PhrasesFromNotes(sp, 0.35), sp, nil)
	if len(lyr[2].Syls) != 3 { // hel / lo / beau+ti+ful
		t.Fatalf("last note: %+v", lyr[2])
	}
	if tok := sylToken(lyr[2].Syls); tok != "beautiful" {
		t.Errorf("token %q", tok)
	}
}

func TestPhrasesFromSpans(t *testing.T) {
	notes := []Span{{0, 0.3}, {0.3, 0.6}, {2, 2.3}, {2.3, 2.6}, {5, 5.2}}
	ph := PhrasesFromSpans(notes, [][2]float64{{0, 1}, {1.9, 3}})
	if len(ph) != 3 || ph[0] != (Phrase{0, 1}) || ph[1] != (Phrase{2, 3}) || ph[2] != (Phrase{4, 4}) {
		t.Fatalf("phrases %+v", ph)
	}
}

func sampleScore() Score {
	g := steady(0, 4)
	notes := []audioanalysis.SungNote{
		noteAt(0, 0, 1, 60), noteAt(0, 1, 1, 62), noteAt(0, 2, 2, 64),
		noteAt(0, 4, 0.5, 65), noteAt(0, 4.5, 0.5, 64), noteAt(0, 5, 3, 62),
		noteAt(0, 8, 6, 60),
	}
	notes[3].Conf = 0.2
	sh := Quantize(notes, g, four, Options{FirstBar: 0, LastBar: 3})
	sp := make([]Span, len(sh.Notes))
	for i, p := range sh.Notes {
		sp[i] = Span{notes[p.Src].Start, notes[p.Src].End}
	}
	lines := []Line{line("0:0", "hello my dear friend, a song")}
	fits, lyr := AlignLines(lines, PhrasesFromNotes(sp, 0.35), sp, nil)
	low := map[int]bool{}
	for i, f := range fits {
		if f.Conf < LowLineConf {
			low[i] = true
		}
	}
	k, _ := KeyOf("G", 0, false)
	return Score{Sheet: sh, Lyrics: lyr, LowLines: low, Key: k, Tempo: 120, Sections: map[int]string{0: "Verse 1", 2: "Chorus"}}
}

func TestAlphaTex(t *testing.T) {
	s := sampleScore()
	tex, marks := s.AlphaTex()
	for _, want := range []string{`\ks (g)`, `\ts (4 4)`, `\section ("Verse 1")`, `\section ("Chorus")`, "C4.4", "E4.2", "C4{t}.2", `\lyrics ("hel- lo my dear friend, a song")`} {
		if !strings.Contains(tex, want) {
			t.Errorf("no %q in\n%s", want, tex)
		}
	}
	if len(marks) == 0 || !marks[0].LowNote || marks[0].Bar != 1 {
		t.Errorf("marks %+v", marks)
	}
	// Transposed up a tone, in A: no flats, F# and C#.
	s.Transpose = 2
	s.Key, _ = KeyOf("G", 2, false)
	tex, _ = s.AlphaTex()
	if !strings.Contains(tex, `\ks (a)`) || !strings.Contains(tex, "D4.4") || !strings.Contains(tex, "F#4") {
		t.Errorf("transposed:\n%s", tex)
	}
	// A man's voice (an octave down): the treble clef an octave lower.
	s.Transpose = -12
	s.Key, _ = KeyOf("G", -12, false)
	if tex, _ = s.AlphaTex(); !strings.Contains(tex, `\ottava (8vb)`) || !strings.Contains(tex, "C3.4") {
		t.Errorf("low voice:\n%s", tex)
	}
	// Bb: flats.
	k, _ := KeyOf("F", 0, false)
	if st, alt, oct := k.spell(70); st != "B" || alt != -1 || oct != 4 {
		t.Errorf("70 in F: %s %d %d", st, alt, oct)
	}
	if k, _ := KeyOf("Em", 0, false); k.alphaTex() != "eminor" || k.Fifths != 1 {
		t.Errorf("Em: %+v", k)
	}
	if k, _ := KeyOf("F#", 0, true); k.Fifths != -6 {
		t.Errorf("F# with flats: %+v", k)
	}
}

// TestAlphaTexRendersInAlphaTab: the generated alphaTex parses in the
// alphaTab the page uses (node), its lyrics on the notes.
func TestAlphaTexRendersInAlphaTab(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	at, _ := filepath.Abs("../webassets/static/js/vendor/alphatab/alphaTab.min.js")
	tex, _ := sampleScore().AlphaTex()
	f := filepath.Join(t.TempDir(), "m.tex")
	if err := os.WriteFile(f, []byte(tex), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `
const a = require(process.argv[1]);
const imp = new a.importer.AlphaTexImporter();
imp.initFromString(require('fs').readFileSync(process.argv[2], 'utf8'), new a.Settings());
const score = imp.readScore();
const out = [];
for (const b of score.tracks[0].staves[0].bars) out.push(b.voices[0].beats.map((be) => (be.isRest ? 'r' : be.notes[0].realValue + (be.notes[0].isTieDestination ? '~' : '')) + (be.lyrics ? '[' + be.lyrics[0] + ']' : '')).join(' '));
console.log(out.join(' | '));`
	out, err := exec.Command(node, "-e", script, at, f).CombinedOutput()
	if err != nil {
		t.Fatalf("alphaTab: %v\n%s\n%s", err, out, tex)
	}
	got := strings.TrimSpace(string(out))
	want := "60[hel-] 62[lo] 64[my] | 65[dear] 64[friend,] 62[a] | 60[song] | 60~[] 62~[]"
	if !strings.HasPrefix(got, "60[hel-] 62[lo] 64[my] | 65[dear] 64[friend,] 62[a] | 60[song]") {
		t.Errorf("alphaTab read\n%s\nwant\n%s\nfrom\n%s", got, want, tex)
	}
}

func TestMusicXML(t *testing.T) {
	x := string(sampleScore().MusicXML())
	for _, want := range []string{"<fifths>1</fifths>", "<rehearsal>Chorus</rehearsal>", "<syllabic>begin</syllabic><text>hel</text>", `<tie type="start"/>`, `<notehead parentheses="yes">`} {
		if !strings.Contains(x, want) {
			t.Errorf("no %q in MusicXML", want)
		}
	}
}
