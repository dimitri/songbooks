package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// spaceClient signs in as personID on h and GETs paths, not following
// redirects.
func spaceClient(t *testing.T, h http.Handler, token string) func(string) *httptest.ResponseRecorder {
	return func(path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
}

// TestBandSections: a band's six sections, its old addresses redirected
// to the view that holds them now, and no sidebar group of its own.
func TestBandSections(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	player, err := st.CreatePerson(ctx, "Bea Player", "bea@example.com")
	if err != nil {
		t.Fatal(err)
	}
	band, err := st.CreateBand(ctx, "Night Owls", player)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := st.CreateSession(ctx, player)
	if err != nil {
		t.Fatal(err)
	}
	get := spaceClient(t, h, tok)
	b := fmt.Sprintf("/bands/%d", band)
	for from, to := range map[string]string{
		b + "/proposals":                  b + "/catalog/vote",
		b + "/listening-party":            b + "/catalog/discover",
		b + "/rehearsals":                 b + "/recordings/rehearsals",
		b + "/rehearsals?date=2026-03-13": b + "/recordings/rehearsals?date=2026-03-13",
		b + "/concerts":                   b + "/recordings/concerts",
		b + "/recordings":                 b + "/recordings/rehearsals",
	} {
		w := get(from)
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != to {
			t.Errorf("GET %s: %d to %q, want 301 to %q", from, w.Code, w.Header().Get("Location"), to)
		}
	}
	// The Songbook's views are chips at the head of its list.
	for path, want := range map[string][]string{
		b:                            {`class="space-tab active" href="` + b + `" aria-current="page"`},
		b + "/catalog":               {`href="` + b + `/catalog" class="space-view active" aria-current="page"`},
		b + "/catalog/vote":          {`href="` + b + `/catalog/vote" class="space-view active" aria-current="page"`, `class="space-tab active" href="` + b + `/catalog"`},
		b + "/catalog/discover":      {`href="` + b + `/catalog/discover" class="space-view active" aria-current="page"`},
		b + "/recordings/concerts":   {`href="` + b + `/recordings/concerts" class="active" aria-current="page"`, `class="space-tab active" href="` + b + `/recordings/rehearsals"`},
		b + "/recordings/rehearsals": {`href="` + b + `/recordings/rehearsals" class="active" aria-current="page"`},
	} {
		w := get(path)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s: %d", path, w.Code)
			continue
		}
		body := w.Body.String()
		for _, s := range want {
			if !strings.Contains(body, s) {
				t.Errorf("GET %s lacks %q", path, s)
			}
		}
		if n := strings.Count(body, `<a class="space-tab`); n != 6 {
			t.Errorf("GET %s: %d section tabs, want 6", path, n)
		}
		if strings.Contains(body, "sidebar-song-group") {
			t.Errorf("GET %s: the band has a sidebar group of its own again", path)
		}
	}
}

// TestCommunitySections: a jam's community has its events first, its
// Downloads are its Resources (the old addresses redirect), and its Admin
// is behind ⚙ for its staff only.
func TestCommunitySections(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	staff, err := st.CreatePerson(ctx, "Jo Staff", "jo@example.com")
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreatePerson(ctx, "Max Member", "max@example.com")
	if err != nil {
		t.Fatal(err)
	}
	jam, err := st.CreateCommunity(ctx, "Jam", staff)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddCommunityMember(ctx, jam, member, "member", staff); err != nil {
		t.Fatal(err)
	}
	c := fmt.Sprintf("/communities/%d", jam)
	tokens := map[int64]string{}
	for _, p := range []int64{staff, member} {
		if tokens[p], err = st.CreateSession(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	get := spaceClient(t, h, tokens[member])
	for from, to := range map[string]string{
		c + "/downloads":                  c + "/resources",
		c + "/downloads/songbooks/12":     c + "/resources/songbooks/12",
		c + "/downloads/songbooks/12?x=1": c + "/resources/songbooks/12?x=1",
	} {
		w := get(from)
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != to {
			t.Errorf("GET %s: %d to %q, want 301 to %q", from, w.Code, w.Header().Get("Location"), to)
		}
	}
	for who, gear := range map[int64]bool{staff: true, member: false} {
		get := spaceClient(t, h, tokens[who])
		for _, path := range []string{c, c + "/events", c + "/songbooks", c + "/resources"} {
			w := get(path)
			if w.Code != http.StatusOK {
				t.Errorf("GET %s: %d", path, w.Code)
				continue
			}
			body := w.Body.String()
			if !strings.Contains(body, `class="space-tab active" href="`+path+`" aria-current="page"`) {
				t.Errorf("GET %s: its tab isn't the open one", path)
			}
			tabs := body[strings.Index(body, `class="space-tabs"`):]
			tabs = tabs[:strings.Index(tabs, "</nav>")]
			if strings.Index(tabs, `href="`+c+`/events"`) > strings.Index(tabs, `href="`+c+`/songbooks"`) {
				t.Errorf("GET %s: Events isn't before Songbooks", path)
			}
			if strings.Contains(body, `class="space-gear`) != gear || strings.Contains(tabs, "/admin") {
				t.Errorf("GET %s: Admin behind the gear %v, want %v (and never a tab)", path, !gear, gear)
			}
			if strings.Contains(body, "sidebar-song-group") {
				t.Errorf("GET %s: a sidebar group of the community's own", path)
			}
		}
	}
}

// TestCoachingStudioSections: the coach's six sections and the student's,
// each page of the studio under the right one, with the right view.
func TestCoachingStudioSections(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	coach, err := st.CreatePerson(ctx, "Cora Coach", "cora@example.com")
	if err != nil {
		t.Fatal(err)
	}
	student, err := st.CreatePerson(ctx, "Stan Student", "stan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	studio, err := st.CreateCommunity(ctx, "Studio", coach)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCommunityKind(ctx, studio, "coaching"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddCommunityMember(ctx, studio, student, "member", coach); err != nil {
		t.Fatal(err)
	}
	c := fmt.Sprintf("/communities/%d", studio)
	tabsOf := func(body string) string {
		i := strings.Index(body, `class="space-tabs"`)
		if i < 0 {
			return ""
		}
		tabs := body[i:]
		return tabs[:strings.Index(tabs, "</nav>")]
	}
	open := func(body string) string {
		tabs := tabsOf(body)
		i := strings.Index(tabs, `aria-current="page"`)
		if i < 0 {
			return ""
		}
		j := strings.LastIndex(tabs[:i], `href="`)
		return tabs[j+len(`href="`) : i-2]
	}
	view := func(body string) string {
		i := strings.Index(body, `class="space-filters"`)
		if i < 0 {
			return ""
		}
		f := body[i:]
		f = f[:strings.Index(f, "</nav>")]
		j := strings.Index(f, `class="active" aria-current="page"`)
		if j < 0 {
			return "none"
		}
		k := strings.LastIndex(f[:j], `href="`)
		return f[k+len(`href="`) : j-2]
	}
	tok, _ := st.CreateSession(ctx, coach)
	get := spaceClient(t, h, tok)
	for path, want := range map[string][2]string{
		c + "/students":            {c + "/students", c + "/students?view=cards"},
		c + "/students?view=table": {c + "/students", c + "/students?view=table"},
		c + "/student-songs":       {c + "/students", c + "/student-songs"},
		c + "/groups":              {c + "/students", c + "/groups"},
		c + "/lessons":             {c + "/lessons", c + "/lessons"},
		c + "/events":              {c + "/lessons", c + "/events"},
		c + "/warmups":             {c + "/warmups", ""},
		c + "/members":             {"", ""},
	} {
		w := get(path)
		if w.Code != http.StatusOK {
			t.Errorf("coach, GET %s: %d", path, w.Code)
			continue
		}
		body := w.Body.String()
		if got := open(body); got != want[0] {
			t.Errorf("coach, GET %s: open tab %q, want %q", path, got, want[0])
		}
		if got := view(body); got != want[1] {
			t.Errorf("coach, GET %s: open view %q, want %q", path, got, want[1])
		}
		if n := strings.Count(tabsOf(body), "<a "); n != 6 {
			t.Errorf("coach, GET %s: %d sections, want 6", path, n)
		}
		if strings.Contains(tabsOf(body), "/members") || !strings.Contains(body, `class="space-gear`) {
			t.Errorf("coach, GET %s: Members isn't behind ⚙ Admin", path)
		}
	}
	if w := get(c + "/members"); !strings.Contains(w.Body.String(), `class="space-gear active"`) {
		t.Errorf("coach: the members page isn't under ⚙ Admin")
	}

	tok, _ = st.CreateSession(ctx, student)
	get = spaceClient(t, h, tok)
	mine := fmt.Sprintf("%s/students/%d", c, student)
	for path, want := range map[string]string{
		c:             c,
		mine:          mine,
		c + "/events": c + "/lessons",
	} {
		w := get(path)
		if w.Code != http.StatusOK {
			t.Errorf("student, GET %s: %d", path, w.Code)
			continue
		}
		body := w.Body.String()
		if got := open(body); got != want {
			t.Errorf("student, GET %s: open tab %q, want %q", path, got, want)
		}
		tabs := tabsOf(body)
		if strings.Count(tabs, "<a ") != 6 || strings.Contains(tabs, `href="`+c+`/students"`) || strings.Contains(body, `class="space-gear`) {
			t.Errorf("student, GET %s: sections %s", path, tabs)
		}
	}
}
