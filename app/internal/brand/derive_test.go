package brand

import "testing"

// A look derived from covers reads at WCAG AA, whatever the covers.
func TestDeriveContrast(t *testing.T) {
	cases := map[string][]Song{
		"dark blues": {
			{Colors: []Color{{"#1a1410", 0.6}, {"#c8401e", 0.3}, {"#e8d8b0", 0.1}}, Genre: "Blues"},
			{Colors: []Color{{"#0c0c10", 0.7}, {"#b03a28", 0.2}, {"#2a5a8a", 0.1}}, Genre: "Soul"},
		},
		"light pop": {
			{Colors: []Color{{"#f4efe6", 0.6}, {"#1e7fd0", 0.3}, {"#ffd23f", 0.1}}, Genre: "Pop"},
			{Colors: []Color{{"#ffffff", 0.5}, {"#2a8be0", 0.4}, {"#111111", 0.1}}, Genre: "Dance"},
		},
		"greys": {
			{Colors: []Color{{"#777777", 0.5}, {"#222222", 0.5}}, Genre: "Rock"},
		},
	}
	for name, songs := range cases {
		d := Derive(songs)
		b := d.Brand
		for _, c := range []struct{ what, fg, bg string }{
			{"text on background", b.Text, b.Background},
			{"secondary text on background", b.Muted, b.Background},
			{"text on header", contrastOn(b.Header), b.Header},
			{"text on accent button", contrastOn(b.Accent), b.Accent},
			{"accent on background", b.Accent, b.Background},
			{"tagline on header", b.Accent2, b.Header},
		} {
			if r := contrast(c.fg, c.bg); r < 4.5 {
				t.Errorf("%s: %s %s on %s is %.2f:1", name, c.what, c.fg, c.bg, r)
			}
		}
		if Clean(b) != b {
			t.Errorf("%s: not a clean brand: %+v", name, b)
		}
	}
	if d := Derive(cases["dark blues"]); d.Mood != "dark" || d.FontDisplay != "fraunces" {
		t.Errorf("dark blues: %s, %s", d.Mood, d.FontDisplay)
	}
	if d := Derive(cases["light pop"]); d.Mood != "light" || d.Shape != "pill" {
		t.Errorf("light pop: %s, %s", d.Mood, d.Shape)
	}
	if d := Derive(nil); !d.Brand.IsZero() {
		t.Errorf("no songs: %+v", d)
	}
}

// OKLCH there and back is the same color.
func TestOKLCHRoundTrip(t *testing.T) {
	for _, hex := range []string{"#af4e39", "#8fd0f2", "#121212", "#ffffff", "#cc1259"} {
		l, c, h := oklch(hex)
		if got := lch(l, c, h); got != hex {
			t.Errorf("%s → %s", hex, got)
		}
	}
}
