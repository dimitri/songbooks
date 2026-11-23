package melody

import (
	"os"
	"strings"
	"testing"

	"app/internal/audioanalysis"
)

// TestExtremesFirstOccurrence: the lowest and the highest notes are the
// first sung, in played order (a repeat's later pass loses to the first),
// the unsure notes left out while there are sure ones; their bars are the
// played bars they start in.
func TestExtremesFirstOccurrence(t *testing.T) {
	g := steady(0, 6)
	notes := []audioanalysis.SungNote{
		noteAt(0, 0, 1, 60),  // bar 0
		noteAt(0, 5, 1, 55),  // bar 1: the lowest, first
		noteAt(0, 9, 1, 72),  // bar 2: the highest, first
		noteAt(0, 13, 1, 55), // bar 3: as low, later
		noteAt(0, 17, 1, 72), // bar 4: as high, later (a repeat's pass)
		{Start: 21 * 0.5, End: 22 * 0.5, Pitch: 40, Conf: 0.1}, // bar 5: lower, unsure
	}
	sh := Quantize(notes, g, four, Options{FirstBar: -1, LastBar: -1})
	lo, hi, ok := sh.Extremes()
	if !ok || sh.Notes[lo].Midi != 55 || sh.Notes[hi].Midi != 72 {
		t.Fatalf("extremes: %d %d %v (%+v)", lo, hi, ok, sh.Notes)
	}
	if b := sh.BarOf(lo); b < 0 || sh.Bars[b].Played != 1 {
		t.Errorf("the lowest's bar: %d", b)
	}
	if b := sh.BarOf(hi); b < 0 || sh.Bars[b].Played != 2 {
		t.Errorf("the highest's bar: %d", b)
	}
	// Only unsure notes: they count.
	all := Quantize([]audioanalysis.SungNote{{Start: 0, End: 0.5, Pitch: 50, Conf: 0.1}, {Start: 1, End: 1.5, Pitch: 52, Conf: 0.1}}, g, four, Options{FirstBar: -1, LastBar: -1})
	if lo, hi, ok := all.Extremes(); !ok || all.Notes[lo].Midi != 50 || all.Notes[hi].Midi != 52 {
		t.Errorf("unsure only: %d %d %v", lo, hi, ok)
	}
	// Within the melody's known range: the octave-off 40 and the 72s left
	// out, the 60 of bar 0 the highest.
	if lo, hi, ok := sh.ExtremesWithin(50, 65); !ok || sh.Notes[lo].Midi != 55 || sh.Notes[hi].Midi != 60 || sh.Bars[sh.BarOf(hi)].Played != 0 {
		t.Errorf("within 50–65: %d %d %v", lo, hi, ok)
	}
	if lo, _, ok := sh.ExtremesWithin(80, 90); !ok || sh.Notes[lo].Midi != 55 {
		t.Errorf("nothing within: all of them, %d %v", lo, ok)
	}
	if _, _, ok := (Sheet{}).Extremes(); ok {
		t.Error("an empty sheet has extremes")
	}
}

// TestRangeSVG: the range as two whole notes on one bar, moved by the
// key, a low voice on treble-8vb (its 8), the label for screen readers.
func TestRangeSVG(t *testing.T) {
	k, _ := KeyOf("C", 0, false)
	svg := RangeSVG(57, 76, 0, k, "Range A3–E5")
	if strings.Count(svg, "<ellipse") != 2 || !strings.Contains(svg, `aria-label="Range A3–E5"`) || strings.Contains(svg, ">8</text>") {
		t.Errorf("a high voice's range: %s", svg)
	}
	low := RangeSVG(43, 60, 0, k, "Range G2–C4")
	if !strings.Contains(low, ">8</text>") {
		t.Errorf("a low voice isn't on treble-8vb: %s", low)
	}
	// Two semitones down: the high note an E♭ in a flat key, a D in C.
	kf, _ := KeyOf("F", -2, false)
	if svg := RangeSVG(57, 65, -2, kf, ""); !strings.Contains(svg, "♭") {
		t.Errorf("E♭ moved down from F isn't flat: %s", svg)
	}
	writeDebug(t, "range", svg+low)
}

// TestBarSVG: a bar as sung — its notes, its rest, the marked note,
// the syllables under it, moved by the key.
func TestBarSVG(t *testing.T) {
	g := steady(0, 2)
	notes := []audioanalysis.SungNote{
		noteAt(0, 0, 1, 64), noteAt(0, 1, 0.5, 67), noteAt(0, 1.5, 0.5, 69), noteAt(0, 2, 1, 72),
	}
	sh := Quantize(notes, g, four, Options{FirstBar: 0, LastBar: 0})
	k, _ := KeyOf("C", 0, false)
	lyr := make([]NoteLyric, len(sh.Notes))
	lyr[0] = NoteLyric{Syls: []Syl{{Text: "Pa", WordStart: true}}, Line: 0}
	lyr[1] = NoteLyric{Syls: []Syl{{Text: "per", WordEnd: true}}, Line: 0}
	lyr[2] = NoteLyric{Syls: []Syl{{Text: "boats", WordStart: true, WordEnd: true}}, Line: 0}
	sc := Score{Sheet: sh, Lyrics: lyr, Key: k}
	_, hi, _ := sh.Extremes()
	svg := sc.BarSVG(0, map[int]bool{hi: true}, "bar 1")
	for _, want := range []string{`class="rr-hi"`, ">Pa-</text>", ">per</text>", ">boats</text>", `stroke-linejoin="round"`} {
		if !strings.Contains(svg, want) {
			t.Errorf("the bar: no %q in %s", want, svg)
		}
	}
	if n := strings.Count(svg, "<ellipse"); n != 4 {
		t.Errorf("heads: %d", n)
	}
	// Without lyrics, no text; moved up a semitone in C♯'s spelling.
	sc.Lyrics = nil
	sc.Transpose = 1
	sc.Key, _ = KeyOf("C", 1, false)
	plain := sc.BarSVG(0, map[int]bool{hi: true}, "")
	if strings.Contains(plain, "rr-lyrics") || strings.Contains(plain, "♮") {
		t.Errorf("no lyrics, no stray naturals: %s", plain)
	}
	if sc.BarSVG(5, nil, "") != "" {
		t.Error("a bar past the sheet drawn")
	}
	writeDebug(t, "bar", svg+plain)
}

// writeDebug writes the drawings to $ROWRANGE_SVG_OUT/<name>.html, to
// look at them.
func writeDebug(t *testing.T, name, svgs string) {
	dir := os.Getenv("ROWRANGE_SVG_OUT")
	if dir == "" {
		return
	}
	page := `<!doctype html><meta charset="utf-8"><style>body{font:14px sans-serif;padding:20px}svg{margin:8px;border:1px dotted #ccc}.rr-staff{stroke:currentColor;stroke-width:.6;opacity:.5}.rr-hi{fill:#7c3aed;stroke:#7c3aed;color:#7c3aed}</style>` + svgs
	if err := os.WriteFile(dir+"/"+name+".html", []byte(page), 0o644); err != nil {
		t.Log(err)
	}
}
