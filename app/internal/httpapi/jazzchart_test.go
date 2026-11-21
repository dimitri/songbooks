package httpapi

import (
	"strings"
	"testing"

	"app/internal/song"
	"app/internal/webassets"
)

// Chord symbols the iReal Pro way (chordsym.DecomposeQualityJazz): minor
// as "−", m7b5 as −7♭5 and a written ø (or iReal's "h") as ø7, a minor-
// major chord as −△7, "+" as the augmented sign; the accidentals as music
// glyphs, the slash bass below with its own pitch class for the page's
// transpose.
func TestJazzChordHTML(t *testing.T) {
	for sym, want := range map[string][]string{
		"Bm7b5":    {jzGlyphMin, ">7<", jzGlyphFlat + "</span>5", `data-pc="11"`},
		"Bø":       {jzGlyphHalfDim, ">7<"},
		"Bh7":      {jzGlyphHalfDim, ">7<"},
		"Ebmaj7/G": {jzGlyphFlat, jzGlyphMaj, `jz-b`, `data-pc="7"`},
		"CmMaj7":   {jzGlyphMin + `</span><span class="jz-g">` + jzGlyphMaj + "</span>7<"},
		"Cm(maj7)": {jzGlyphMin + `</span><span class="jz-g">` + jzGlyphMaj + "</span>7<"},
		"C-^7":     {jzGlyphMin + `</span><span class="jz-g">` + jzGlyphMaj + "</span>7<"},
		"C^7":      {jzGlyphMaj + "</span>7<"},
		"CM7":      {jzGlyphMaj + "</span>7<"},
		"C+7":      {jzGlyphAug + "</span>7<"},
		"Cmin7":    {jzGlyphMin + "</span>7<"},
		"F#m":      {jzGlyphSharp, jzGlyphMin},
		"C7b9":     {`jz-acc2`},
		"N.C.":     {"N.C."},
		"Gsus4":    {"sus4"},
	} {
		got := string(jzChordHTML(sym))
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", sym, got, w)
			}
		}
	}
	// "Cm7b5" is not half-diminished here, nor "CMaj7" plain text.
	for sym, not := range map[string]string{"Cm7b5": jzGlyphHalfDim, "CMaj7": "Maj", "CmMaj7": "Maj", "C+7": "+"} {
		if got := string(jzChordHTML(sym)); strings.Contains(got, not) {
			t.Errorf("%s: %q has %q", sym, got, not)
		}
	}
}

// What a screen reader says for a chord, in place of the music glyphs.
func TestJazzChordSpoken(t *testing.T) {
	say := func(ws []jzWord) string {
		var b strings.Builder
		for _, w := range ws {
			if w.Key != "" {
				b.WriteString("<" + w.Key + ">")
			} else {
				b.WriteString(w.Text)
			}
		}
		return b.String()
	}
	for sym, want := range map[string]string{
		"Bbm7":   "B <chord|flat>|<chord|minor> 7|",
		"Cm7":    "C|<chord|minor> 7|",
		"Bb7/D":  "B <chord|flat>|7|D",
		"F#m7b5": "F <chord|sharp>|<chord|minor> 7 <chord|flat> 5|",
		"Bø7":    "B|<chord|half-diminished>|",
		"Cdim7":  "C|<chord|diminished> 7|",
		"C+":     "C|<chord|augmented>|",
		"CmMaj7": "C|<chord|minor> <chord|major> 7|",
		"Ebmaj7": "E <chord|flat>|<chord|major> 7|",
		"C^":     "C|<chord|major> 7|",
		"C7(b9)": "C|7 <chord|flat> 9|",
		"Gsus4":  "G|sus4|",
		"N.C.":   "<chord|no chord>||",
		"C":      "C||",
	} {
		r, q, b := jzChordSpoken(sym)
		if got := say(r) + "|" + say(q) + "|" + say(b); got != want {
			t.Errorf("%s: says %q, want %q", sym, got, want)
		}
	}
}

// The chart renders through the page's template: the header (style,
// composer), the played bars on every printed bar, the road signs, the
// whole-rest sign, the rehearsal marks.
func TestJazzChartTemplate(t *testing.T) {
	sg := song.Parse(jzHead + "style: Medium Swing\ntempo: 120\nartist: Someone\nkey: C\n" + `
[Verse]
grid: | C | F | C | G |
[Chorus]
grid: | F | G | C | R |
[Bridge]
grid: | Dm | Em | F | G |
[Verse]
[Chorus]
[Outro]
grid: | F | G | C | C7 |
`)
	ch := buildJazzChart(sg, buildSections(sg))
	var b strings.Builder
	if err := webassets.Templates.ExecuteTemplate(&b, "jazz-chart", ch); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	for _, w := range []string{"Medium Swing", "Someone", `data-played="0 12"`, "D.C. al Coda", "jz-tocoda", "jz-coda", "jz-rest", `class="jz-rm"`, `aria-label="D minor"`, `data-say-q="minor"`, `aria-label="Whole-bar rest"`, `aria-label="To Coda"`} {
		if !strings.Contains(html, w) {
			t.Errorf("chart lacks %q", w)
		}
	}
}
