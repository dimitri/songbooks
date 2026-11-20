package httpapi

import (
	"strings"
	"testing"

	"app/internal/store"
)

// The search sits in the global navigation: the sidebar's field at its
// top, and the phone top bar's 🔍 on Accueil and the Library's segments
// only.
func TestNavSearchPlacement(t *testing.T) {
	me := &store.Person{ID: 1, DisplayName: "Lucas"}
	sidebar := renderTemplate(t, "desktop-sidebar", map[string]any{"Me": me, "Nav": navState{Section: "home", Me: me, Page: "index"}})
	brand, field, primary := strings.Index(sidebar, `class="sidebar-brand"`), strings.Index(sidebar, `class="nav-search"`), strings.Index(sidebar, `class="sidebar-primary"`)
	if !(brand >= 0 && brand < field && field < primary) {
		t.Errorf("the search field isn't at the sidebar's top (brand %d, field %d, entries %d)", brand, field, primary)
	}
	for page, want := range map[string]bool{
		"index": true, "catalog": true, "songbooks-index": true, "setlists-index": true,
		"calendar_page": false, "band": false, "notifications": false,
	} {
		bar := renderTemplate(t, "global-tabbar", map[string]any{"Me": me, "Nav": navState{Me: me, Page: page}})
		actions := bar[strings.Index(bar, `id="topbar-actions"`):]
		got := strings.Contains(actions, `class="nav-search-button"`)
		if got != want {
			t.Errorf("%s: top bar search %v, want %v", page, got, want)
		}
		if got && strings.Index(actions, `class="nav-search-button"`) > strings.Index(actions, `data-topbar-slot="notifications"`) {
			t.Errorf("%s: the search should come before the bell", page)
		}
	}
}
