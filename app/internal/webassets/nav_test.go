package webassets

import "testing"

func TestDocTitle(t *testing.T) {
	for _, c := range []struct {
		parts []string
		want  string
	}{
		{nil, "Songbooks"},
		{[]string{"Agenda", "Velvet Hour"}, "Agenda — Velvet Hour — Songbooks"},
		{[]string{"", "Studio Lumen"}, "Studio Lumen — Songbooks"},
		{[]string{"Aujourd’hui"}, "Aujourd’hui — Songbooks"},
	} {
		if got := DocTitle(c.parts...); got != c.want {
			t.Errorf("DocTitle(%q) = %q, want %q", c.parts, got, c.want)
		}
	}
}

// One name per destination: no two entries of a surface share a label or
// a path, and every entry names its section.
func TestNavListsOneNamePerDestination(t *testing.T) {
	for _, surface := range []string{"desktop", "mobileTab", "mobileMore"} {
		labels, paths := map[string]bool{}, map[string]bool{}
		for _, e := range navGlobal(surface) {
			if labels[e.Label] || paths[e.Path] {
				t.Errorf("%s: %s (%s) listed twice", surface, e.Label, e.Path)
			}
			labels[e.Label], paths[e.Path] = true, true
			if e.Section == "" {
				t.Errorf("%s: %s has no section", surface, e.Label)
			}
		}
	}
}
