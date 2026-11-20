package webassets

import (
	"fmt"
	"testing"
)

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

// The phone's bar is the proposal's five tabs, and every section has an
// entry on every surface (its own, or one that covers it).
func TestNavSurfaces(t *testing.T) {
	var bar []string
	for _, e := range navGlobal("bar") {
		bar = append(bar, e.Label)
	}
	if got, want := fmt.Sprint(bar), "[Home Library Calendar Groups Tools]"; got != want {
		t.Errorf("phone bar %s, want %s", got, want)
	}
	sections := []string{NavHome, NavCalendar, NavLibrary, NavBands, NavCommunities, NavGroups, NavTools,
		NavMetronome, NavTuner, NavNotifications}
	for _, surface := range []string{"sidebar", "bar"} {
		entries := navGlobal(surface)
		if surface == "sidebar" {
			entries = append(entries, navGlobal("sidebarTools")...)
		}
		for _, sec := range sections {
			if surface == "sidebar" && (sec == NavTools || sec == NavGroups || sec == NavBands || sec == NavCommunities) {
				continue // the sidebar lists the tools, and the groups (sidebar-spaces), themselves
			}
			n := 0
			for _, e := range entries {
				if NavCurrentOn(surface, e, sec) {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%s: %d entries current for %q, want 1", surface, n, sec)
			}
		}
	}
	// Home stands for the notifications on the phone only: the sidebar
	// has their own entry.
	home := navEntries[0]
	if !NavCurrentOn("bar", home, NavNotifications) || NavCurrentOn("sidebar", home, NavNotifications) {
		t.Error("Home covers the notifications on the phone's bar only")
	}
}

// One name per destination: no two entries of a surface share a label or
// a path, and every entry names its section.
func TestNavListsOneNamePerDestination(t *testing.T) {
	for _, surface := range []string{"sidebar", "sidebarTools", "bar"} {
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
