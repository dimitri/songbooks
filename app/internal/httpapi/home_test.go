package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"app/internal/covers"
	"app/internal/store"
)

// TestComingUpWeek: a weekly rehearsal shows once, at its next date, as
// weekly; a date repeating irregularly says how many more there are;
// only this week's show, and the next ones when nothing is on this week.
func TestComingUpWeek(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip(err)
	}
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, paris) // a Monday
	band, other := int64(1), int64(2)
	at := func(day, hour, min int) time.Time { return time.Date(2026, 11, day, hour, min, 0, 0, paris) }
	var items []store.CalendarItem
	// Other Lies' rehearsal, every Wednesday at 20:00 for six weeks (the
	// last one in December, past the month's end).
	for i := 0; i < 6; i++ {
		items = append(items, store.CalendarItem{Kind: "rehearsal", Title: "Répétition", Starts: at(25+7*i, 20, 0), BandID: &band, GroupName: "Other Lies"})
	}
	items = append(items,
		// A gig on Saturday.
		store.CalendarItem{Kind: "gig", Title: "Fête", Starts: at(28, 0, 0), AllDay: true, BandID: &other},
		// A jam on Friday, then again three weeks later: not weekly.
		store.CalendarItem{Kind: "event", Title: "Jam", Starts: at(27, 21, 0)},
		store.CalendarItem{Kind: "event", Title: "Jam", Starts: at(27+21, 21, 0)},
		// Over already (this morning at 7).
		store.CalendarItem{Kind: "rehearsal", Title: "Early", Starts: at(23, 7, 0), BandID: &other},
	)
	sortByStart(items)
	got := comingUpWeek(items, now)
	if got.Later || got.Total != 9 {
		t.Fatalf("Later %v, Total %d; want false, 9", got.Later, got.Total)
	}
	var titles []string
	for _, r := range got.Rows {
		titles = append(titles, r.Title)
	}
	if want := []string{"Répétition", "Jam", "Fête"}; !reflect.DeepEqual(titles, want) {
		t.Fatalf("rows %v, want %v", titles, want)
	}
	if r := got.Rows[0]; !r.Weekly || r.More != 5 || !r.Starts.Equal(at(25, 20, 0)) {
		t.Errorf("the rehearsal: weekly %v, more %d, at %v", r.Weekly, r.More, r.Starts)
	}
	if r := got.Rows[1]; r.Weekly || r.More != 1 {
		t.Errorf("the jam: weekly %v, more %d", r.Weekly, r.More)
	}

	// Nothing this week: the next dates after it.
	var later []store.CalendarItem
	for _, it := range items {
		if it.Starts.After(at(30, 23, 0)) {
			later = append(later, it)
		}
	}
	got = comingUpWeek(later, now)
	if !got.Later || len(got.Rows) == 0 || got.Rows[0].Title != "Répétition" {
		t.Fatalf("nothing this week: %+v", got)
	}
	if got := comingUpWeek(nil, now); got.Later || len(got.Rows) != 0 || got.Total != 0 {
		t.Fatalf("nothing at all: %+v", got)
	}
}

func sortByStart(items []store.CalendarItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Starts.Before(items[j-1].Starts); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// TestChoosePicture: the card's picture, by the band's choice; the
// covers are only loaded when they may show.
func TestChoosePicture(t *testing.T) {
	four := []store.MosaicCover{{WorkID: 1}, {WorkID: 2}, {WorkID: 3}, {WorkID: 4}, {WorkID: 5}}
	for _, c := range []struct {
		name       string
		choice     store.CardImage
		logo       string
		covers     []store.MosaicCover
		wantLogo   string
		wantMosaic int
		loads      bool
	}{
		{"auto, a logo", store.CardImage{}, "/l.png", four, "/l.png", 0, false},
		{"auto, no logo", store.CardImage{}, "", four, "", 4, true},
		{"auto, nothing", store.CardImage{}, "", nil, "", 0, true},
		{"logo", store.CardImage{Mode: store.CardLogo}, "", four, "", 0, false},
		{"mosaic over a logo", store.CardImage{Mode: store.CardCoverMosaic}, "/l.png", four, "", 4, true},
		{"mosaic, no covers", store.CardImage{Mode: store.CardCoverMosaic}, "/l.png", nil, "/l.png", 0, true},
		{"picked", store.CardImage{Mode: store.CardPicked, Works: []int64{5}}, "/l.png", four, "", 1, true},
	} {
		loaded := false
		logo, mosaic := choosePicture(c.choice, c.logo, func() []store.MosaicCover { loaded = true; return c.covers })
		if logo != c.wantLogo || len(mosaic) != c.wantMosaic || loaded != c.loads {
			t.Errorf("%s: logo %q, %d covers, loaded %v; want %q, %d, %v", c.name, logo, len(mosaic), loaded, c.wantLogo, c.wantMosaic, c.loads)
		}
	}
}

// TestSortGroupCards: a date coming up first, soonest first, then names.
func TestSortGroupCards(t *testing.T) {
	soon, later := time.Now().Add(time.Hour), time.Now().Add(48*time.Hour)
	cards := []groupCard{
		{Name: "zeta"}, {Name: "Alpha"},
		{Name: "Later", Next: &store.CalendarItem{Starts: later}},
		{Name: "Soon", Next: &store.CalendarItem{Starts: soon}},
	}
	sortGroupCards(cards)
	var names []string
	for _, c := range cards {
		names = append(names, c.Name)
	}
	if want := []string{"Soon", "Later", "Alpha", "zeta"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("%v, want %v", names, want)
	}
}

// TestCardImageAccess: every member of a band sees its card picture's
// choice, only its core members change it, to its own songs' covers;
// the home page then shows them on the band's card.
func TestCardImageAccess(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	person := func(name string) *store.Person {
		id, err := st.CreatePerson(ctx, name, name+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		p, err := st.GetPersonByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	core, guest, outsider := person("core"), person("guest"), person("outsider")
	bid, err := st.CreateBand(ctx, "Other Lies", core.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBandMember(ctx, bid, guest.ID, "guest", core.ID); err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateBand(ctx, "Elsewhere", outsider.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Five songs with covers, and one of another band.
	cover := func(band, by int64, title, image string) int64 {
		vid, err := st.CreateBandVariant(ctx, band, title, "Artist "+title, by)
		if err != nil {
			t.Fatal(err)
		}
		sb, _ := st.GetDefaultSongbook(ctx, "band", band)
		songs, err := st.SongbookCovers(ctx, sb)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range songs {
			if s.VariantID == vid {
				if err := st.SetWorkCover(ctx, s.WorkID, &covers.Cover{Source: "itunes", Image: image}); err != nil {
					t.Fatal(err)
				}
				return s.WorkID
			}
		}
		t.Fatalf("no work for %q", title)
		return 0
	}
	var ours []int64
	for _, title := range []string{"E", "D", "C", "B", "A"} {
		ours = append(ours, cover(bid, core.ID, title, "https://img.example/"+title+".jpg"))
	}
	theirs := cover(other, outsider.ID, "X", "https://img.example/X.jpg")

	do := func(p *store.Person, method, body string) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, "/api/bands/"+itoa(bid)+"/card-image", strings.NewReader(body))
		} else {
			r = httptest.NewRequest(method, "/api/bands/"+itoa(bid)+"/card-image", nil)
		}
		w := httptest.NewRecorder()
		NewServer(st, nil, p, "", "").ServeHTTP(w, r)
		return w
	}
	if w := do(guest, "GET", ""); w.Code != http.StatusOK {
		t.Fatalf("GET by a guest: %d %s", w.Code, w.Body)
	}
	if w := do(outsider, "GET", ""); w.Code != http.StatusForbidden {
		t.Errorf("GET by an outsider: %d, want 403", w.Code)
	}
	for _, p := range []*store.Person{guest, outsider} {
		if w := do(p, "POST", `{"mode":"mosaic"}`); w.Code != http.StatusForbidden {
			t.Errorf("POST by %s: %d, want 403", p.DisplayName, w.Code)
		}
	}
	if w := do(core, "POST", `{"mode":"picked","works":[`+itoa(theirs)+`]}`); w.Code != http.StatusBadRequest {
		t.Errorf("picking another band's cover: %d, want 400", w.Code)
	}
	w := do(core, "POST", `{"mode":"picked","works":[`+itoa(ours[0])+`,`+itoa(theirs)+`]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("POST by core: %d %s", w.Code, w.Body)
	}
	var got cardImageState
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != store.CardPicked || !reflect.DeepEqual(got.Works, []int64{ours[0]}) || len(got.Choices) != 5 || got.Choices[0].Title != "A" {
		t.Fatalf("saved: %+v", got)
	}

	// The home page: the band's card shows the picked cover only.
	home := func() string {
		r := httptest.NewRequest("GET", "/home", nil)
		w := httptest.NewRecorder()
		NewServer(st, nil, guest, "", "").ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("home: %d %s", w.Code, w.Body)
		}
		return w.Body.String()
	}
	page := home()
	if !strings.Contains(page, `https://img.example/E.jpg`) || strings.Contains(page, `https://img.example/A.jpg`) {
		t.Errorf("the picked cover isn't the card's alone")
	}
	// Automatic: the first four by title, a 2×2 mosaic.
	if w := do(core, "POST", `{"mode":""}`); w.Code != http.StatusOK {
		t.Fatalf("POST automatic: %d", w.Code)
	}
	page = home()
	for _, title := range []string{"A", "B", "C", "D"} {
		if !strings.Contains(page, "https://img.example/"+title+".jpg") {
			t.Errorf("automatic mosaic: no %s", title)
		}
	}
	if strings.Contains(page, "https://img.example/E.jpg") || !strings.Contains(page, "group-pic m4") {
		t.Errorf("automatic mosaic isn't the first four, 2×2")
	}
}

// TestGroupLine: the line that matters for each kind of card.
func TestGroupLine(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip(err)
	}
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, paris)
	rehearsal := &store.CalendarItem{Kind: "rehearsal", Title: "Répétition", Starts: time.Date(2026, 11, 25, 20, 0, 0, 0, paris)}
	gig := &store.CalendarItem{Kind: "gig", Title: "Fête", Starts: time.Date(2026, 11, 28, 0, 0, 0, 0, paris), AllDay: true}
	for _, c := range []struct {
		lang, kind string
		f          groupFacts
		want       string
		waiting    bool
	}{
		{"en", "band", groupFacts{Next: rehearsal, OpenVotes: 2}, "Next: rehearsal Wed 25, 20:00", true},
		{"en", "band", groupFacts{Next: gig}, "Next: gig Sat 28", true},
		{"en", "band", groupFacts{OpenVotes: 1, Songs: 3, Members: 4}, "1 song to vote on", true},
		{"en", "band", groupFacts{Songs: 3, Members: 1}, "3 songs · 1 member", false},
		{"en", "community", groupFacts{Next: rehearsal, Lessons: 1, NewSongs: 2}, "1 lesson this week · 2 new songs this week", true},
		{"en", "community", groupFacts{NewSongs: 1}, "1 new song this week", true},
		{"en", "community", groupFacts{Next: rehearsal, Songs: 12, Members: 30}, "12 songs · 30 members", false},
		{"fr", "band", groupFacts{Next: rehearsal}, "À venir : répétition mer. 25, 20:00", true},
	} {
		got, waiting := groupLine(c.lang, c.kind, c.f, now)
		if got != c.want || waiting != c.waiting {
			t.Errorf("groupLine(%s, %s, %+v) = %q, %v; want %q, %v", c.lang, c.kind, c.f, got, waiting, c.want, c.waiting)
		}
	}
}
