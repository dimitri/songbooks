package httpapi

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"app/internal/store"
	"app/internal/webassets"
)

func TestBuildNavSpaces(t *testing.T) {
	now := time.Now()
	ago := func(h int) *time.Time { v := now.Add(-time.Duration(h) * time.Hour); return &v }
	spaces := []store.Space{
		{Kind: "band", ID: 3, Name: "Couleur Acoustique", Pinned: true, UsedAt: ago(48)},
		{Kind: "community", ID: 7, Name: "Studio Lumen", CommunityKind: "coaching", UsedAt: ago(1)},
		{Kind: "community", ID: 4, Name: "Jam du Canal", CommunityKind: "community", UsedAt: ago(5)},
	}
	for i := 0; i < 5; i++ {
		spaces = append(spaces, store.Space{Kind: "band", ID: int64(20 + i), Name: fmt.Sprintf("Band %d", i)})
	}
	n := buildNavSpaces(spaces, "community", 4)
	if len(n.All) != 8 || len(n.Shown) != sidebarSpaces || n.More() != 2 {
		t.Fatalf("all %d, shown %d, more %d", len(n.All), len(n.Shown), n.More())
	}
	if n.Current == nil || n.Current.Name != "Jam du Canal" || !n.All[2].Current {
		t.Errorf("current: %+v", n.Current)
	}
	// The Groups tab reopens the one used last, pinned or not.
	if n.LastURL != "/communities/7" {
		t.Errorf("last used: %q", n.LastURL)
	}
	want := map[int64][2]string{3: {"/bands/3", "Band"}, 7: {"/communities/7", "Coaching"}, 4: {"/communities/4", "Community"}}
	for _, v := range n.All[:3] {
		if w := want[v.ID]; v.URL != w[0] || v.Sub != w[1] {
			t.Errorf("%s: %s %s, want %v", v.Name, v.URL, v.Sub, w)
		}
	}
	if n.All[0].Initials != "CA" || n.All[0].Tone < 0 || n.All[0].Tone > 7 {
		t.Errorf("avatar: %+v", n.All[0])
	}
	// Outside a space, nothing is current; never used, no last.
	n = buildNavSpaces(spaces[3:], "", 0)
	if n.Current != nil || n.LastURL != "" || n.More() != 0 {
		t.Errorf("outside: %+v", n)
	}
	if n := buildNavSpaces(nil, "band", 3); len(n.All) != 0 || len(n.Shown) != 0 {
		t.Errorf("no spaces: %+v", n)
	}
}

func TestPageSpace(t *testing.T) {
	var none *store.Band
	for _, c := range []struct {
		data map[string]any
		kind string
		id   int64
	}{
		{map[string]any{"Band": &store.Band{ID: 3}}, "band", 3},
		{map[string]any{"Band": store.Band{ID: 3}}, "band", 3},
		{map[string]any{"Community": &store.Community{ID: 7}}, "community", 7},
		{map[string]any{"Band": none}, "", 0},
		{map[string]any{"Path": "/bands/17/setlists/10"}, "band", 17},
		{map[string]any{"Path": "/setlists/10"}, "", 0},
		{map[string]any{}, "", 0},
	} {
		if k, id := pageSpace(c.data); k != c.kind || id != c.id {
			t.Errorf("%v: %s %d", c.data, k, id)
		}
	}
}

// The sidebar lists the spaces and marks the current one; the switcher
// lists them all with "All my groups" first; the phone's Groups tab opens
// the last used.
func TestSpacesRender(t *testing.T) {
	me := &store.Person{ID: 1, DisplayName: "Lucas"}
	spaces := []store.Space{
		{Kind: "band", ID: 3, Name: "Couleur Acoustique", Pinned: true},
		{Kind: "community", ID: 7, Name: "Studio Lumen", CommunityKind: "coaching", UsedAt: &time.Time{}},
	}
	nav := navState{Section: "communities", Me: me, Spaces: buildNavSpaces(spaces, "community", 7)}
	data := map[string]any{"Me": me, "Nav": nav}
	sidebar := renderTemplate(t, "desktop-sidebar", data)
	if m := regexp.MustCompile(`<a href="([^"]+)" class="sidebar-space[^"]*" aria-current="page"`).FindAllStringSubmatch(sidebar, -1); len(m) != 1 || m[0][1] != "/communities/7" {
		t.Errorf("sidebar's current space: %v", m)
	}
	if !strings.Contains(sidebar, `href="/groups"`) {
		t.Error("the sidebar lacks All my groups")
	}
	bar := renderTemplate(t, "global-tabbar", data)
	if !strings.Contains(bar, `<a href="/communities/7" class="active" aria-current="page" data-label="Groupes"`) {
		t.Error("the Groups tab doesn't reopen the last used space")
	}
	if !strings.Contains(bar, `class="space-switcher space-switcher--topbar"`) || !strings.Contains(bar, `aria-pressed="true"`) {
		t.Error("the top bar's switcher, or its pinned space, is missing")
	}
	sw := renderTemplate(t, "space-switcher", map[string]any{"Nav": nav, "Where": "header"})
	if strings.Index(sw, `href="/groups"`) > strings.Index(sw, `href="/bands/3"`) {
		t.Error("All my groups isn't at the top of the switcher")
	}
	if !strings.Contains(sw, `space-switcher-name">Studio Lumen`) {
		t.Error("the header switcher doesn't name the space")
	}
}

func renderTemplate(t *testing.T, name string, data any) string {
	t.Helper()
	var b strings.Builder
	if err := webassets.TemplatesFor("fr").ExecuteTemplate(&b, name, data); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return b.String()
}
