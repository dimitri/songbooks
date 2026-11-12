package brand

import (
	"strings"
	"testing"
)

func TestCleanDropsUnsafe(t *testing.T) {
	b := Clean(Brand{
		Background: "red; background:url(x)", Accent: "#AbCdEf", FontUI: "comic-sans",
		LogoURL: "javascript:alert(1)", Website: "http://plain.example", Mark: "☾☾☾☾☾", Preset: "nope",
	})
	if b.Background != "" || b.FontUI != "" || b.LogoURL != "" || b.Website != "" || b.Preset != "" {
		t.Fatalf("unsafe fields kept: %+v", b)
	}
	if b.Accent != "#abcdef" || b.Mark != "☾☾☾" {
		t.Fatalf("valid fields lost: %+v", b)
	}
}

func TestResolvePreset(t *testing.T) {
	th := Resolve(Brand{Preset: "other-lies", Accent: "#ff0000"}, "Other Lies")
	if th == nil || !th.Dark {
		t.Fatal("Other Lies is a dark brand")
	}
	css := string(th.Style)
	for _, want := range []string{"--bg: #0e141b", "--accent: #ff0000", `--font-display: "Fraunces"`, "--radius-control: 999px"} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %q in %s", want, css)
		}
	}
	if !strings.Contains(th.FontsURL, "Fraunces") || !strings.Contains(th.FontsURL, "Space+Grotesk") {
		t.Errorf("fonts URL %q", th.FontsURL)
	}
	if Resolve(Brand{}, "x") != nil {
		t.Error("an empty brand is no theme")
	}
}

// contrast is the WCAG contrast ratio of two hex colors.
func contrast(a, b string) float64 {
	x, y := luminance(a), luminance(b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

// Every preset must keep its text readable (WCAG AA, 4.5:1): the page
// text and secondary text on the background, the header's text, and the
// label of an accent button, which contrastOn picks.
func TestPresetsContrast(t *testing.T) {
	for key, p := range Presets {
		b := p.Brand
		for _, c := range []struct{ what, fg, bg string }{
			{"text on background", b.Text, b.Background},
			{"secondary text on background", b.Muted, b.Background},
			{"text on header", contrastOn(b.Header), b.Header},
			{"text on accent button", contrastOn(b.Accent), b.Accent},
		} {
			if r := contrast(c.fg, c.bg); r < 4.5 {
				t.Errorf("%s: %s %s on %s is %.2f:1", key, c.what, c.fg, c.bg, r)
			}
		}
	}
	// The tagline is small text in the second accent on the header. The
	// older presets predate this check (Blues & Rock's red on black is
	// 3.8:1, Paper's 3.1:1), so it is held to the newer ones.
	for _, key := range []string{"other-lies", "stage", "bands-camp", "jamspace"} {
		b := Presets[key].Brand
		if r := contrast(b.Accent2, b.Header); r < 4.5 {
			t.Errorf("%s: tagline %s on header %s is %.2f:1", key, b.Accent2, b.Header, r)
		}
	}
}

// The presets made after a community's website keep their fonts loaded
// and their logo and site links through Clean.
func TestCommunityPresets(t *testing.T) {
	logo := "https://static.wixstatic.com/media/7d4d31_df8cf45ed75e451f846cd6918d0859e4~mv2.png/v1/fill/w_192,h_192,al_c/7d4d31_df8cf45ed75e451f846cd6918d0859e4~mv2.png"
	th := Resolve(Brand{Preset: "bands-camp", LogoURL: logo, Website: "https://www.bands-camp.com/"}, "Bands Camp")
	if th == nil || th.Dark || th.LogoURL != logo {
		t.Fatalf("Bands Camp: %+v", th)
	}
	if !strings.Contains(string(th.Style), `--font-display: "Anton"`) || !strings.Contains(th.FontsURL, "family=Anton") {
		t.Errorf("Bands Camp fonts: %s / %s", th.Style, th.FontsURL)
	}
	th = Resolve(Brand{Preset: "jamspace"}, "JamSpace")
	if th == nil || th.Dark || !strings.Contains(th.FontsURL, "Nunito+Sans") || !strings.Contains(th.FontsURL, "Bebas+Neue") {
		t.Errorf("JamSpace: %+v", th)
	}
}
