package httpapi

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"app/internal/song"
)

// jazzFormOf parses src and returns its chart form, checking the one
// thing every chart must do: read back to exactly the song's played
// bars, bar for bar (buildJazzForm only keeps a chart that does).
func jazzFormOf(t *testing.T, src string) jzFormResult {
	t.Helper()
	sg := song.Parse(src)
	secs := buildSections(sg)
	total := 0
	for _, s := range secs {
		total += s.BarCount
	}
	jf := jazzFormFor(sg, secs)
	if jf == nil {
		t.Fatalf("no chart")
	}
	if len(jf.res.played) != total {
		t.Fatalf("the chart plays %d bars, the song %d: %s", len(jf.res.played), total, jf.res.describe())
	}
	return jf.res
}

func wantForm(t *testing.T, src, want string) jzFormResult {
	t.Helper()
	res := jazzFormOf(t, src)
	if got := res.describe(); got != want {
		t.Errorf("form\n got: %s\nwant: %s", got, want)
	}
	return res
}

const jzHead = "title: T\ntime: 4/4\n"

// A verse/chorus pair played twice, then an outro: one repeat around
// both sections, each printed once.
func TestJazzFormVerseChorusRepeat(t *testing.T) {
	wantForm(t, jzHead+`
[Intro]
grid: | C | F | C | G |
[Verse]
grid: | Am | F | C | G |
[Chorus]
grid: | F | G | C | Am |
[Verse]
[Chorus]
[Outro]
grid: | F | G | C | C |
`, "[Intro].... [Verse]|:.... [Chorus]....:| [Outro]....")
}

// Verse and chorus come back after a bridge, then the song ends on its
// own outro: § on the verse, To Coda after the chorus, D.S. al Coda
// after the bridge, the outro as the coda. (With the verse first, the
// same road map from the top is a D.C. — see the tests below.)
func TestJazzFormDSAlCoda(t *testing.T) {
	wantForm(t, jzHead+`
[Intro]
grid: | C | F | C | G |
[Verse]
grid: | Am | F | C | G | Am | F | C | G |
[Chorus]
grid: | F | G | C | Am | F | G | C | C |
[Bridge]
grid: | Dm | Em | F | G |
[Verse]
[Chorus]
[Outro]
grid: | F | G | C | C |
`, "[Intro].... [Verse]§........ [Chorus]........→Φ [Bridge].... D.S. al Coda Φ[Outro]....")
}

// The song starts again from the top and ends at the end of its first
// part: D.C. al Fine.
func TestJazzFormDCAlFine(t *testing.T) {
	wantForm(t, jzHead+`
[A]
grid: | C | F | C | G | C | F | G | C |
[B]
grid: | Am | Em | F | G |
[A]
`, "[A]........Fine [B].... D.C. al Fine")
}

// A section played twice in a row is one repeat; three times, "3x" —
// whether written as times:, as a bare repeat, or both.
func TestJazzFormSectionTwiceInARow(t *testing.T) {
	wantForm(t, jzHead+`
[Verse]
grid: | Am | F | C | G |
[Verse]
[Chorus]
grid: | F | G | C | C |
times: 3
`, "[Verse]|:....:| [Chorus]|:....:|3x")
	// A grid that is already one repeat: its count goes up.
	wantForm(t, jzHead+`
[Chorus]
grid: |: F | G | C | C :|
[Chorus] (repeat)
`, "[Chorus]|:....:|4x")
}

// Sections with the same chords are one printed section, whatever they
// are called: its box carries every name ("In/V"), and the form repeats
// it like any section — here twice in a row, then (as A and V taking
// turns over one grid) five times, its own repeat raised to 10x.
func TestJazzFormSameChordsDifferentNames(t *testing.T) {
	src := jzHead + `
[Intro]
grid: | Am | F | C | G |
[Verse]
grid: | Am | F | C | G |
[Chorus]
grid: |: F | G | C | C :|
[Verse]
grid: |: F | G | C | C :|
[Chorus] (repeat)
[Verse]
[Chorus]
`
	wantForm(t, src, "[Intro]|:....:| [Chorus]|:....:|10x")
	sg := song.Parse(src)
	ch := buildJazzChart(sg, buildSections(sg))
	var marks []string
	for _, l := range ch.Lines {
		for _, c := range l.Cells {
			if c.Mark != "" {
				marks = append(marks, c.Mark+" "+c.Name)
			}
		}
	}
	if got := strings.Join(marks, ", "); got != "In/V Intro / Verse, A/V Chorus / Verse" {
		t.Errorf("marks: %s", got)
	}
}

// A section with its own endings can't sit inside another repeat sign:
// played twice in a row it's printed once, "2x" by its closing barline,
// and read back as the whole section twice.
func TestJazzFormSectionRepeatOverEndings(t *testing.T) {
	res := wantForm(t, jzHead+`
[Intro]
grid: |: Dm | Gm | (1.) A7 | Dm :| (2.) Gm | A7 |
[Verse]
grid: |: Dm | Gm | (1.) A7 | Dm :| (2.) Gm | A7 |
[Chorus]
grid: | F | C | Gm | A7 |
`, "[Intro]|:..(1.)..:|(2.)..‖2x [Chorus]....")
	if got := len(res.played); got != 2*8+4 {
		t.Errorf("%d bars played, want 20", got)
	}
}

// The last chorus is the outro: it starts like the chorus and ends its
// own way. The D.S. goes back to the verse; To Coda sits inside the
// chorus where they part, and the coda holds only the outro's own bars.
func TestJazzFormOutroEndOfChorus(t *testing.T) {
	wantForm(t, jzHead+`
[Verse]
grid: | Am | F | C | G | Am | F | C | G |
[Chorus]
grid: | F | G | C | Am | F | G | C | C |
[Bridge]
grid: | Dm | Em | F | G | Dm | Em | F | G |
[Verse]
[Outro]
grid: | F | G | C | Am | F | G | Am | Am |
`, "[Verse]........ [Chorus]......→Φ.. [Bridge]........ D.C. al Coda Φ[Outro]..")
}

// A verse coming back with a different last bar, straight after itself:
// one verse with first and second endings.
func TestJazzFormEndingsSameSection(t *testing.T) {
	wantForm(t, jzHead+`
[Verse]
grid: | Am | F | C | G |
[Verse]
grid: | Am | F | C | E7 |
[Chorus]
grid: | F | G | C | C |
`, "[Verse]|:...(1.).:|(2.). [Chorus]....")
}

// Verse + chorus twice, the second chorus ending differently: the
// repeat covers both, the endings only the bars that differ.
func TestJazzFormGroupWithEndings(t *testing.T) {
	wantForm(t, jzHead+`
[Verse]
grid: | Am | F | C | G |
[Chorus]
grid: | F | G | C | C |
[Verse]
[Chorus]
grid: | F | G | Dm | G7 |
[Bridge]
grid: | Em | Em | F | F |
`, "[Verse]|:.... [Chorus]..(1.)..:|(2.).. [Bridge]....")
}

// Grids with their own endings keep them, drawn the iReal way (the
// repeat sign closing each ending played again), and still take part in
// the form: a D.C./D.S. over them says the repeats are taken again.
func TestJazzFormVoltasInGrids(t *testing.T) {
	res := wantForm(t, jzHead+`
[Verse]
grid: |: Am | F | (1.) C | G :| (2.) C | E7 |
[Chorus]
grid: | F | G | C | C |
[Bridge]
grid: | Dm | Em | F | G |
[Verse]
[Chorus]
[Outro]
grid: | F | G | C | C7 |
`, "[Verse]|:..(1.)..:|(2.).. [Chorus]....→Φ [Bridge].... D.C. al Coda Φ[Outro]....")
	jump := false
	for _, c := range res.cells {
		if c.jump != "" {
			jump = c.withRepeats
		}
	}
	if !jump {
		t.Errorf("D.C. over a repeat should say the repeats are taken again")
	}
	// The iReal export's way, the repeat sign after the last ending:
	// passes 1-3 take the first ending, the 4th the second.
	ms := song.Parse(jzHead + "[A]\ngrid: |: (1,2,3.) Ab | Gm | (4.) Bb | Bb :|\n").Sections[0].Measures
	if got := fmt.Sprint(jzExpand(ms)); got != "[0 1 0 1 0 1 2 3]" {
		t.Errorf("iReal-style endings played %s", got)
	}
	cells := jzWrittenCells(0, ms)
	if !cells[1].close || cells[3].close {
		t.Errorf("the repeat sign should close the first ending, not the last")
	}
	// One ending for every pass is a plain repeat: "4x".
	ms = song.Parse(jzHead + "[A]\ngrid: |: (1,2,3,4.) B | A . E . :|\n").Sections[0].Measures
	if got := len(jzExpand(ms)); got != 8 {
		t.Errorf("(1,2,3,4.) over the whole bracket played %d bars, want 8", got)
	}
	if c := jzWrittenCells(0, ms)[1]; c.countText != 4 || c.endStart {
		t.Errorf("want a 4x repeat, got %+v", c)
	}
	// A last-pass-only ending: the bars before it are repeated.
	ms = song.Parse(jzHead + "[A]\ngrid: |: Gm | Bb | (4.) C :|\n").Sections[0].Measures
	if got := fmt.Sprint(jzExpand(ms)); got != "[0 1 0 1 0 1 0 1 2]" {
		t.Errorf("(4.) ending played %s", got)
	}
}

// When nothing fits, sections are printed again, in order — never a bar
// lost.
func TestJazzFormFallsBackToPrintingAgain(t *testing.T) {
	wantForm(t, jzHead+`
[A]
grid: | C | F | C | G |
[B]
grid: | Am | Em | F | G |
[A]
[C]
grid: | Dm | G | C | C |
[B]
`, "[A].... [B].... [A].... [C].... [B]....")
}

// Whatever the sequence, the chart reads back to every played bar, in
// order: random forms over a few sections, some with their own repeats,
// endings, times: counts.
func TestJazzFormNeverLosesABar(t *testing.T) {
	grids := []string{
		"| C | F | C | G |",
		"| Am | F | C | G | Am | F | C | E7 |",
		"|: F | G | C | Am :|",
		"|: Dm | G | (1.) C | A7 :| (2.) C | C |",
		"| F | G | C | Am | F | G | C | C7 |",
		"|: (1,2,3.) Eb | Bb | (4.) Ab | Ab :|",
		"| Em | % | Am | %% |",
	}
	names := []string{"Intro", "Verse", "Chorus", "Bridge", "Solo", "Outro"}
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 400; trial++ {
		var b strings.Builder
		b.WriteString(jzHead)
		gridOf := map[string]string{}
		n := 3 + rng.Intn(9)
		for i := 0; i < n; i++ {
			name := names[rng.Intn(len(names))]
			fmt.Fprintf(&b, "[%s]\n", name)
			g, ok := gridOf[name]
			switch {
			case !ok || rng.Intn(5) == 0:
				g = grids[rng.Intn(len(grids))]
				gridOf[name] = g
				fmt.Fprintf(&b, "grid: %s\n", g)
			case rng.Intn(3) == 0:
				fmt.Fprintf(&b, "grid: %s\n", g)
			}
			if rng.Intn(6) == 0 {
				fmt.Fprintf(&b, "times: %d\n", 2+rng.Intn(2))
			}
		}
		src := b.String()
		sg := song.Parse(src)
		secs := buildSections(sg)
		total := 0
		for _, s := range secs {
			total += s.BarCount
		}
		jf := jazzFormFor(sg, secs)
		if jf == nil {
			if total > 0 {
				t.Fatalf("no chart for\n%s", src)
			}
			continue
		}
		if len(jf.res.played) != total {
			t.Fatalf("trial %d: %d bars played, want %d\n%s\n%s", trial, len(jf.res.played), total, src, jf.res.describe())
		}
		// Every played bar maps to a song bar number once, in order.
		for i, g := range jf.globalOf {
			if g != i {
				t.Fatalf("trial %d: played bar %d numbered %d", trial, i, g)
			}
		}
	}
}

// The chart's numbering is the song's: after a repeat or a D.S., a
// played bar lights the printed bar it is read from.
func TestJazzChartPlayedBarsMapToCells(t *testing.T) {
	src := jzHead + `
[Verse]
grid: | Am | F | C | G |
[Chorus]
grid: | F | G | C | C |
[Bridge]
grid: | Dm | Em | F | G |
[Verse]
[Chorus]
[Outro]
grid: | F | G | C | C7 |
`
	sg := song.Parse(src)
	secs := buildSections(sg)
	ch := buildJazzChart(sg, secs)
	if ch == nil || !ch.Compact {
		t.Fatalf("want a compact chart")
	}
	byBar := map[int]string{}
	for li, ln := range ch.Lines {
		for ci, c := range ln.Cells {
			for _, p := range strings.Fields(c.Played) {
				n, _ := strconv.Atoi(p)
				byBar[n] = fmt.Sprintf("%d.%d", li, ci)
			}
		}
	}
	// Verse (0-3), Chorus (4-7), Bridge (8-11), D.S.: Verse again (12-15)
	// on the verse's printed bars, Chorus (16-19), coda: Outro (20-23).
	for played, want := range map[int]string{0: "0.0", 12: "0.0", 4: "1.0", 19: "1.3", 8: "2.0", 20: "3.0", 23: "3.3"} {
		if got := byBar[played]; got != want {
			t.Errorf("played bar %d lights %s, want %s", played, got, want)
		}
	}
	if len(byBar) != 24 {
		t.Errorf("%d played bars mapped, want 24", len(byBar))
	}
}

// jzMarksOf is a chart's rehearsal marks with their names, in print order.
func jzMarksOf(ch *jazzChart) string {
	var marks []string
	for _, l := range ch.Lines {
		for _, c := range l.Cells {
			if c.Mark != "" {
				marks = append(marks, c.Mark+" "+c.Name)
			}
		}
	}
	return strings.Join(marks, ", ")
}

// An outro whose first bars are the chorus's, played from a D.C. to the
// coda, doesn't lend its name to the chorus: no "Chorus / Outro" box,
// the outro named once (on the coda), and the marks in print order.
func TestJazzChartOutroNamedOnItsCoda(t *testing.T) {
	sg := song.Parse(jzHead + `
[Verse]
grid: | Am | F | C | G | Am | F | C | G |
[Chorus]
grid: | F | G | C | Am | F | G | C | C |
[Bridge]
grid: | Dm | Em | F | G | Dm | Em | F | G |
[Verse]
[Outro]
grid: | F | G | C | Am | F | G | Am | Am |
note: slowing down
`)
	ch := buildJazzChart(sg, buildSections(sg))
	if got := jzMarksOf(ch); got != "V Verse, A Chorus, B Bridge, C Outro" {
		t.Errorf("marks: %s", got)
	}
	// The outro's note is the coda's.
	found := false
	for _, l := range ch.Lines {
		for _, n := range l.Notes {
			found = true
			if n != "slowing down" || !l.Coda {
				t.Errorf("note %q on a line (coda %v)", n, l.Coda)
			}
		}
	}
	if !found {
		t.Errorf("the outro's note is lost")
	}
}

// Notes from each pass of a repeated verse say which verse they are.
func TestJazzChartNotesSayWhichPass(t *testing.T) {
	sg := song.Parse(jzHead + `
[Verse]
grid: | Am | F | C | G |
note: softly
[Chorus]
grid: | F | G | C | C |
[Verse]
note: loud
[Chorus]
`)
	ch := buildJazzChart(sg, buildSections(sg))
	var notes []string
	for _, l := range ch.Lines {
		notes = append(notes, l.Notes...)
	}
	if got := strings.Join(notes, " | "); got != "Verse 1: softly | Verse 2: loud" {
		t.Errorf("notes: %s", got)
	}
}

// A one-bar meter change is followed by the section's time again, never
// by the previous section's over the next one's own; a run of {3/4} bars
// shows its time once.
func TestJazzChartMeterChanges(t *testing.T) {
	ts := func(src string) string {
		sg := song.Parse(src)
		ch := buildJazzChart(sg, buildSections(sg))
		var out []string
		for _, l := range ch.Lines {
			for _, c := range l.Cells {
				if c.Pad {
					continue
				}
				s := "."
				if c.TSNum != "" {
					s = c.TSNum + "/" + c.TSDen
				}
				out = append(out, s)
			}
		}
		return strings.Join(out, " ")
	}
	if got := ts(jzHead + "[A]\ngrid: | C | {2/4} G |\n[B]\ntime: 3/4\ngrid: | F | F |\n"); got != "4/4 2/4 3/4 ." {
		t.Errorf("after {2/4}, B's own 3/4: %s", got)
	}
	if got := ts(jzHead + "[A]\ngrid: | C | {2/4} G | C | C |\n"); got != "4/4 2/4 4/4 ." {
		t.Errorf("{2/4} then back to 4/4: %s", got)
	}
	if got := ts(jzHead + "[A]\ngrid: | {3/4} C | {3/4} G | {3/4} C | {3/4} F |\n"); got != "3/4 . . ." {
		t.Errorf("a run of {3/4}: %s", got)
	}
}

// The last ending, after the ":|", gets iReal's short open bracket — not
// one over the rest of the section — while the form still plays every bar.
func TestJazzChartLastEndingShortBracket(t *testing.T) {
	src := jzHead + "[A]\ngrid: |: C | F |(1.) G | G :|(2.) C | C | Am | Am | Dm | G |\n"
	res := jazzFormOf(t, src)
	if got := fmt.Sprint(jzExpand(song.Parse(src).Sections[0].Measures)); got != "[0 1 2 3 0 1 4 5 6 7 8 9]" {
		t.Errorf("played %s", got)
	}
	_ = res
	sg := song.Parse(src)
	ch := buildJazzChart(sg, buildSections(sg))
	var brackets []string
	for _, l := range ch.Lines {
		for _, c := range l.Cells {
			if c.VoltaSpan > 0 {
				brackets = append(brackets, fmt.Sprintf("%s%d%v", c.Volta, c.VoltaSpan, c.VoltaClosed))
			}
		}
	}
	if got := strings.Join(brackets, " "); got != "1.2true 2.2false" {
		t.Errorf("brackets: %s", got)
	}
}
