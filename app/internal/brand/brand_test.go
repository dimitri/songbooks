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
