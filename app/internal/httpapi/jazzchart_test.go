package httpapi

import (
	"strings"
	"testing"

	"app/internal/song"
	"app/internal/webassets"
)

// Chord symbols: minor as "−", half-diminished as ø7, the accidentals
// as music glyphs, the slash bass below with its own pitch class for
// the page's transpose.
func TestJazzChordHTML(t *testing.T) {
	for sym, want := range map[string][]string{
		"Bm7b5":    {jzGlyphHalfDim, ">7<", `data-pc="11"`},
		"Ebmaj7/G": {jzGlyphFlat, jzGlyphMaj, `jz-b`, `data-pc="7"`},
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
	for _, w := range []string{"Medium Swing", "Someone", `data-played="0 12"`, "D.C. al Coda", "jz-tocoda", "jz-coda", "jz-rest", `class="jz-rm"`} {
		if !strings.Contains(html, w) {
			t.Errorf("chart lacks %q", w)
		}
	}
}
