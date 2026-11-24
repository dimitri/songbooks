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

// At 120 bpm in 4/4: an eighth is 0.25 s, a bar 2 s.
var opts = PlaceOptions{Eighth: func(float64) float64 { return 0.25 }, Bar: func(float64) float64 { return 2 }}

// sung: notes at the times given, each until the next (less a breath of
// 0.05 s), the last 0.45 s; a -1 ends a phrase (a rest).
func sung(times ...float64) []Span {
	var out []Span
	for i, t := range times {
		if t < 0 {
			continue
		}
		end := t + 0.45
		if i+1 < len(times) && times[i+1] >= 0 {
			end = times[i+1] - 0.05
		}
		out = append(out, Span{t, end})
	}
	return out
}

func line(key, text string) Line {
	return Line{Key: key, Text: text, Syls: SplitLine(text, English), Start: math.NaN()}
}

// chorded is a line with its chords: "When the [G]night has come".
func chorded(key, text string) (Line, []ChordMark) {
	var segs []Segment
	for i, part := range strings.Split(text, "[") {
		if i == 0 {
			segs = append(segs, Segment{Text: part})
			continue
		}
		c, t, _ := strings.Cut(part, "]")
		segs = append(segs, Segment{Chord: c, Text: t})
	}
	marks, t := MarksOf(segs, English)
	return Line{Key: key, Text: t, Syls: SplitLine(t, English), Start: math.NaN()}, marks
}

func grid(chords ...string) [][]string {
	out := make([][]string, len(chords))
	for i, c := range chords {
		out[i] = []string{c, c, c, c}
	}
	return out
}

func lyricAt(lyr []NoteLyric, n int) string {
	if lyr[n].Hold {
		return "_"
	}
	if len(lyr[n].Syls) == 0 {
		return ""
	}
	return sylToken(lyr[n].Syls)
}

func TestMarksOf(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		{"When the [G]night has [Em]come", "2:G 4:Em"},
		{"[C]Oh dar[Em]ling", "0:C 2:Em"},
		{"[D]I lo[G]ve you", "0:D 1:G"},
		{"[A]la la [A]la", "0:A 2:A"},
	} {
		_, marks := chorded("0:0", c.in)
		var got []string
		for _, m := range marks {
			got = append(got, fmt.Sprintf("%d:%s", m.Syl, m.Chord))
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%q: %v, want %s", c.in, got, c.want)
		}
	}
}

func TestAnchorLines(t *testing.T) {
	l0, m0 := chorded("0:0", "[G]walking in the [Em]rain")
	l1, m1 := chorded("0:1", "[C]under the [D]sky, [G]oh")
	as := AnchorLines(grid("G", "G", "Em", "Em", "C", "D", "G", "G"), [][]ChordMark{m0, m1}, []int{len(l0.Syls), len(l1.Syls)}, nil)
	if fmt.Sprint(as) != "[[{0 0} {4 2}] [{0 4} {3 5} {4 6}]]" {
		t.Errorf("anchors %v", as)
	}
	// One chord all along: the syllables' share decides which bar.
	l0, m0 = chorded("0:0", "[G]one two three four")
	l1, m1 = chorded("0:1", "[G]five six seven eight")
	as = AnchorLines(grid("G", "G", "G", "G"), [][]ChordMark{m0, m1}, []int{len(l0.Syls), len(l1.Syls)}, nil)
	if fmt.Sprint(as) != "[[{0 0}] [{0 2}]]" {
		t.Errorf("one chord: %v", as)
	}
	// A repeat played twice (the grid as played): the second pass's line
	// on the second pass's bars; a chord the grid doesn't play: unplaced.
	l0, m0 = chorded("0:0", "[Am]first time round")
	l1, m1 = chorded("0:1", "[Am]second time [B7]round")
	as = AnchorLines(grid("Am", "F", "Am", "F"), [][]ChordMark{m0, m1}, []int{len(l0.Syls), len(l1.Syls)}, nil)
	if fmt.Sprint(as) != "[[{0 0}] [{0 2}]]" {
		t.Errorf("repeat: %v", as)
	}
}

// TestAnchorLinesBySinging: a chord that comes round every other bar
// (Am F Am F …) and two lines starting on Am: the syllables' share alone
// would put the second line a pass late; the notes sung (as many as its
// syllables from bar 4, nothing in bars 2–3) put it on bar 4.
func TestAnchorLinesBySinging(t *testing.T) {
	l0, m0 := chorded("0:0", "When the [Am]night has come and the land is dark and I")
	l1, m1 := chorded("0:1", "[Am]see")
	bars := grid("Am", "F", "Am", "F", "Am", "F", "Am", "F")
	syls := []int{len(l0.Syls), len(l1.Syls)}
	// Line 1: a pickup of 2 before bar 0, 11 more in bars 0–1; line 2's one
	// syllable on bar 4 and held.
	onsets := []float64{-0.25, -0.125}
	for k := range 11 {
		onsets = append(onsets, float64(k)*0.125)
	}
	onsets = append(onsets, 4)
	notes := func(a, b float64) int {
		n := 0
		for _, o := range onsets {
			if o >= a-1.0/16 && o < b-1.0/16 {
				n++
			}
		}
		return n
	}
	if as := AnchorLines(bars, [][]ChordMark{m0, m1}, syls, nil); fmt.Sprint(as) == "[[{2 0}] [{0 4}]]" {
		t.Logf("the share alone found it too: %v", as)
	}
	if as := AnchorLines(bars, [][]ChordMark{m0, m1}, syls, notes); fmt.Sprint(as) != "[[{2 0}] [{0 4}]]" {
		t.Errorf("by the notes: %v", as)
	}
}

// TestPlaceLinesPickupDoesNotDrift is the "line 2 drifts a bar" report:
// line 1 ends on a held note and a melisma, line 2 starts with a pickup
// ("and the") sung just before its bar with no rest between: line 2's
// chord syllable stays on its bar, the pickup on the notes before it,
// line 1's melisma doesn't push it on.
func TestPlaceLinesPickupDoesNotDrift(t *testing.T) {
	l0, m0 := chorded("0:0", "When the [G]night has come")
	l1, m1 := chorded("0:1", "and the [Em]land is dark")
	// Bars of 2 s from 2 s: line 1's G on bar 1 (4 s), line 2's Em on bar 3 (8 s).
	l0.Anchors = []Anchor{{m0[0].Syl, 4}}
	l1.Anchors = []Anchor{{m1[0].Syl, 8}}
	l0.Start, l1.Start = 3, 7
	//               When the  night has come (held) melisma  and  the  land is  dark
	notes := sung(3, 3.5, 4, 4.5, 5, 6.5, 7, 7.5, 8, 8.5, 9)
	fits, lyr := PlaceLines([]Line{l0, l1}, notes, opts, nil)
	var got []string
	for i := range notes {
		got = append(got, lyricAt(lyr, i))
	}
	if strings.Join(got, " ") != "When the night has come _ and the land is dark" {
		t.Fatalf("lyrics: %q", strings.Join(got, " "))
	}
	if fits[1].First != 6 || fits[1].AnchorNote != 8 || fits[0].Last != 5 {
		t.Errorf("fits %+v", fits)
	}
}

// TestPlaceLinesShiftedOnsets: the singer pushes the chords' syllables
// an eighth early and drags others: each still lands on its own.
func TestPlaceLinesShiftedOnsets(t *testing.T) {
	l0, m0 := chorded("0:0", "[G]walking in the [Em]rain")
	l1, m1 := chorded("0:1", "[C]under the [D]sky")
	l0.Anchors = []Anchor{{m0[0].Syl, 2}, {m0[1].Syl, 6}}
	l1.Anchors = []Anchor{{m1[0].Syl, 10}, {m1[1].Syl, 12}}
	//              wal   king in   the  rain        un    der  the  sky
	notes := sung(1.8, 2.6, 3.3, 4.1, 5.8, -1, 9.78, 10.6, 11.2, 12.1)
	_, lyr := PlaceLines([]Line{l0, l1}, notes, opts, nil)
	var got []string
	for i := range notes {
		got = append(got, lyricAt(lyr, i))
	}
	if strings.Join(got, " ") != "wal- king in the rain un- der the sky" {
		t.Fatalf("lyrics: %q", strings.Join(got, " "))
	}
}

func TestPlaceLinesWeakAndAdLib(t *testing.T) {
	// No chords: each line where its section's share puts it; an ad-lib
	// between them is left out.
	l0, l1 := line("0:0", "hold me now darling"), line("0:1", "never let me go")
	l0.Start, l1.Start = 2, 6
	notes := sung(2, 2.5, 3, 3.5, 4, -1, 4.9, 5.2, -1, 6.1, 6.5, 7, 7.5, 8)
	fits, lyr := PlaceLines([]Line{l0, l1}, notes, opts, nil)
	if fits[0].First != 0 || fits[0].Last != 4 || fits[1].First != 7 || lyr[5].Line != -1 || lyr[6].Line != -1 {
		t.Fatalf("fits %+v", fits)
	}
	if fits[0].Anchored || fits[0].Conf >= 0.7 {
		t.Errorf("a line placed by its share is less sure: %+v", fits[0])
	}
}

func TestPlaceLinesMissingAndStrict(t *testing.T) {
	l0, m0 := chorded("0:0", "[G]one two three")
	l1, m1 := chorded("0:1", "[C]four five six")
	l0.Anchors = []Anchor{{m0[0].Syl, 2}}
	l1.Anchors = []Anchor{{m1[0].Syl, 10}}
	l0.Start, l1.Start = 2, 10
	// Line 2's notes come 0.8 s after its chord (a held note before it).
	notes := sung(2, 2.5, 3, -1, 10.8, 11.3, 11.8)
	fits, _ := PlaceLines([]Line{l0, l1}, notes, opts, nil)
	if !fits[1].Matched || fits[1].First != 3 {
		t.Fatalf("late but within half a bar: %+v", fits[1])
	}
	strict := opts
	strict.Strict = true
	fits, _ = PlaceLines([]Line{l0, l1}, notes, strict, nil)
	if fits[1].Anchored || fits[1].Conf > fits[0].Conf {
		t.Errorf("strict: %+v", fits[1])
	}
	// Nothing sung near a line: it finds nothing.
	fits, _ = PlaceLines([]Line{l0, l1}, sung(2, 2.5, 3), opts, nil)
	if fits[1].Matched {
		t.Errorf("an unsung line placed: %+v", fits[1])
	}
}

func TestPlaceLinesShiftAndJoin(t *testing.T) {
	l := line("0:0", "la la la")
	l.Start = 0
	notes := sung(0, 0.5, 1, 1.5, 2, 2.5)
	fits, lyr := PlaceLines([]Line{l}, notes, opts, map[string]int{"0:0": 2})
	if fits[0].First != 2 || fits[0].Shift != 2 || lyr[0].Line != -1 || lyricAt(lyr, 2) != "la" {
		t.Fatalf("shift: %+v %+v", fits[0], lyr[:3])
	}
	l = line("0:0", "hello beautiful")
	l.Start = 0
	_, lyr = PlaceLines([]Line{l}, sung(0, 0.5, 1), opts, nil)
	if tok := lyricAt(lyr, 2); tok != "beautiful" { // hel / lo / beau+ti+ful
		t.Errorf("last note %q", tok)
	}
}

func TestInterpolate(t *testing.T) {
	nan := math.NaN()
	got := Interpolate([]float64{nan, 10, nan, nan, 20}, []int{4, 5, 5, 5, 4}, 0, 30)
	if fmt.Sprint(got) != "[0 10 13.333333333333334 16.666666666666668 20]" {
		t.Errorf("interpolated %v", got)
	}
	got = Interpolate([]float64{nan, nan}, []int{3, 3}, 4, 12)
	if fmt.Sprint(got) != "[4 8]" {
		t.Errorf("a section without anchors: %v", got)
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
	l := line("0:0", "hello my dear friend, a song")
	l.Start = 0
	fits, lyr := PlaceLines([]Line{l}, sp, opts, nil)
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

// TestPlaceLinesWeakLineLeavesThePickup: a line placed by its share (an
// unsung line of the section before) never takes the notes an anchored
// line claims, its pickup included.
func TestPlaceLinesWeakLineLeavesThePickup(t *testing.T) {
	l0 := line("0:3", "that I never talk about")
	l0.Start = 3.2
	l1, m1 := chorded("1:0", "On the [Am]night bus")
	l1.Anchors = []Anchor{{m1[0].Syl, 4}}
	l1.Start = 3.5
	notes := sung(3, 3.5, 4, 4.5)
	fits, lyr := PlaceLines([]Line{l0, l1}, notes, opts, nil)
	if fits[0].Matched || lyricAt(lyr, 0) != "On" || lyricAt(lyr, 2) != "night" {
		t.Fatalf("fits %+v, lyrics %q %q", fits, lyricAt(lyr, 0), lyricAt(lyr, 2))
	}
}
