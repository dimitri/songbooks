package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/mailer"
	"app/internal/store"
)

// instrumentsTestStore opens the test database under the lock the store
// package's tests take (one test at a time across packages), emptied;
// skipped without one.
func instrumentsTestStore(t *testing.T) *store.Store {
	t.Helper()
	conn := os.Getenv("SONGBOOKS_TEST_DATABASE_URL")
	if conn == "" {
		conn = "postgres://localhost/songbooks_test?sslmode=disable"
	}
	ctx := context.Background()
	lock, err := pgx.Connect(ctx, conn)
	if err != nil {
		t.Skipf("no test database at %s: %v", conn, err)
	}
	t.Cleanup(func() { lock.Close(context.Background()) })
	if _, err := lock.Exec(ctx, `SELECT pg_advisory_lock(7215)`); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(ctx, conn)
	if err != nil {
		t.Skipf("no test database at %s: %v", conn, err)
	}
	t.Cleanup(st.Close)
	if _, err := lock.Exec(ctx, `TRUNCATE people, bands, communities, file_blobs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	return st
}

// asPerson is an HTTP client logged in as personID (a real session).
type asPerson struct {
	t      *testing.T
	srv    *httptest.Server
	cookie string
}

func newAsPerson(t *testing.T, st *store.Store, srv *httptest.Server, personID int64) asPerson {
	t.Helper()
	tok, err := st.CreateSession(context.Background(), personID)
	if err != nil {
		t.Fatal(err)
	}
	return asPerson{t, srv, tok}
}

func (a asPerson) do(method, path string, form url.Values) (int, string) {
	a.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, a.srv.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept-Language", "fr")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: a.cookie})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestProfileInstruments(t *testing.T) {
	st := instrumentsTestStore(t)
	ctx := context.Background()
	srv := httptest.NewServer(NewServer(st, mailer.NewConsoleMailer(st), nil, "", ""))
	defer srv.Close()

	me, _ := st.CreatePerson(ctx, "me", "Me", "")
	mate, _ := st.CreatePerson(ctx, "mate", "Mate", "")
	stranger, _ := st.CreatePerson(ctx, "stranger", "Stranger", "")
	band, _ := st.CreateBand(ctx, "Mercy", me)
	if _, err := st.AddBandMember(ctx, band, mate, "guest", me); err != nil {
		t.Fatal(err)
	}
	asMe := newAsPerson(t, st, srv, me)
	asMate := newAsPerson(t, st, srv, mate)
	asStranger := newAsPerson(t, st, srv, stranger)

	// Add, with a level; "other" typed as a known instrument is filed there.
	if code, _ := asMe.do("POST", "/profile/instruments/add", url.Values{"instrument": {"guitar"}, "level": {"advanced"}}); code != http.StatusSeeOther {
		t.Fatalf("add guitar: %d", code)
	}
	if code, _ := asMe.do("POST", "/profile/instruments/add", url.Values{"instrument": {"other"}, "other_name": {"Contrebasse"}}); code != http.StatusSeeOther {
		t.Fatalf("add other: %d", code)
	}
	if code, _ := asMe.do("POST", "/profile/instruments/add", url.Values{"instrument": {"kazoo"}}); code != http.StatusSeeOther {
		t.Fatalf("add unknown: %d", code)
	}
	if code, _ := asMe.do("POST", "/profile/instruments/add", url.Values{"instrument": {"bass"}, "level": {"pro"}}); code != http.StatusBadRequest {
		t.Errorf("add with an unknown level: %d, want 400", code)
	}
	mine, _ := st.ListPersonInstruments(ctx, me)
	if len(mine) != 2 || mine[0].Instrument != "guitar" || mine[0].Level != "advanced" || mine[1].Instrument != "double_bass" {
		t.Fatalf("my instruments: %+v", mine)
	}
	guitar := strconv.FormatInt(mine[0].ID, 10)

	// The profile page, in French.
	code, page := asMe.do("GET", "/profile", nil)
	if code != 200 {
		t.Fatalf("profile: %d", code)
	}
	for _, want := range []string{"Mes instruments", "Guitare", "Contrebasse", "Confirmé", "Niveau non précisé", "Débutant", "Je tiens ma partie", "Qui voit vos niveaux"} {
		if !strings.Contains(page, want) {
			t.Errorf("profile page lacks %q", want)
		}
	}

	// Only I change or remove my instruments.
	if code, _ := asMate.do("POST", "/profile/instruments/"+guitar+"/level", url.Values{"level": {"beginner"}}); code != http.StatusNotFound {
		t.Errorf("mate setting my level: %d, want 404", code)
	}
	if code, _ := asStranger.do("POST", "/profile/instruments/"+guitar+"/remove", nil); code != http.StatusNotFound {
		t.Errorf("stranger removing my guitar: %d, want 404", code)
	}
	if code, _ := asMe.do("POST", "/profile/instruments/"+guitar+"/level", url.Values{"level": {"expert"}}); code != http.StatusSeeOther {
		t.Errorf("my level: %d", code)
	}

	// Who sees what.
	type view struct {
		Instruments []struct {
			Instrument, Name, Stem, Level, Source string
		}
	}
	get := func(a asPerson) (int, view) {
		code, body := a.do("GET", "/api/people/"+strconv.FormatInt(me, 10)+"/instruments", nil)
		var v view
		_ = json.Unmarshal([]byte(body), &v)
		return code, v
	}
	if code, v := get(asMate); code != 200 || len(v.Instruments) != 2 || v.Instruments[0].Level != "expert" || v.Instruments[0].Name != "Guitare" || v.Instruments[1].Stem != "bass" {
		t.Errorf("bandmate sees %d %+v", code, v)
	}
	if code, _ := get(asStranger); code != http.StatusNotFound {
		t.Errorf("stranger sees my instruments: %d", code)
	}
	if code, _ := asMe.do("POST", "/profile/instruments/sharing", url.Values{}); code != http.StatusSeeOther {
		t.Fatalf("sharing off: %d", code)
	}
	if code, v := get(asMate); code != 200 || len(v.Instruments) != 2 || v.Instruments[0].Level != "" {
		t.Errorf("bandmate sees levels I don't share: %d %+v", code, v)
	}
	if _, v := get(asMe); v.Instruments[0].Level != "expert" {
		t.Errorf("I don't see my own level: %+v", v)
	}

	if code, _ := asMe.do("POST", "/profile/instruments/"+guitar+"/remove", nil); code != http.StatusSeeOther {
		t.Errorf("remove: %d", code)
	}
	if mine, _ := st.ListPersonInstruments(ctx, me); len(mine) != 1 {
		t.Errorf("after removing: %+v", mine)
	}
}

func TestJamRegistrationCollectsInstruments(t *testing.T) {
	st := instrumentsTestStore(t)
	ctx := context.Background()
	srv := httptest.NewServer(NewServer(st, mailer.NewConsoleMailer(st), nil, "", ""))
	defer srv.Close()

	org, _ := st.CreatePerson(ctx, "org", "Org", "")
	comm, _ := st.CreateCommunity(ctx, "JamSpace", org)
	jam, err := st.CreateEvent(ctx, "community", comm, "jam", "Jam du jeudi", "", "", time.Now().Add(48*time.Hour), nil, "members", org)
	if err != nil {
		t.Fatal(err)
	}
	me, _ := st.CreatePerson(ctx, "me", "Me", "")
	if _, err := st.AddInstrument(ctx, me, "bass", "", "intermediate"); err != nil {
		t.Fatal(err)
	}
	asMe := newAsPerson(t, st, srv, me)
	asOrg := newAsPerson(t, st, srv, org)
	path := "/events/" + strconv.FormatInt(jam, 10)

	code, page := asMe.do("GET", path, nil)
	if code != 200 || !strings.Contains(page, "Ce que vous jouerez") || !strings.Contains(page, `value="bass" checked`) {
		t.Fatalf("jam page: %d, the question or my bass ticked missing", code)
	}
	if code, _ := asMe.do("POST", path+"/register", url.Values{"plays": {"1"}, "instrument": {"bass", "voice"}, "instrument_more": {"harmonica"}}); code != http.StatusFound {
		t.Fatalf("register: %d", code)
	}
	mine, _ := st.ListPersonInstruments(ctx, me)
	got := map[string]store.PersonInstrument{}
	for _, pi := range mine {
		got[pi.Instrument] = pi
	}
	if got["bass"].Level != "intermediate" || got["bass"].Source != "self" {
		t.Errorf("bass changed: %+v", got["bass"])
	}
	if got["harmonica"].Source != "event" || got["harmonica"].SourceName != "Jam du jeudi" || got["voice"].Source != "event" {
		t.Errorf("collected: %+v", mine)
	}
	// The organiser sees what I'll play, with my level; the line-up.
	_, page = asOrg.do("GET", path, nil)
	if !strings.Contains(page, "Basse · Intermédiaire") || !strings.Contains(page, "Harmonica") {
		t.Errorf("organiser's view of the line-up lacks my instruments and level")
	}
	// Changing it afterwards.
	if code, _ := asMe.do("POST", path+"/instruments", url.Values{"instrument": {"bass"}}); code != http.StatusSeeOther {
		t.Errorf("change: %d", code)
	}
	regs, _ := st.ListEventRegistrations(ctx, jam)
	if len(regs) != 1 || strings.Join(regs[0].Instruments, ",") != "bass" {
		t.Errorf("registration now: %+v", regs)
	}
	stranger, _ := st.CreatePerson(ctx, "stranger", "Stranger", "")
	if code, _ := newAsPerson(t, st, srv, stranger).do("POST", path+"/instruments", url.Values{"instrument": {"drums"}}); code != http.StatusForbidden {
		t.Errorf("unregistered: %d, want 403", code)
	}
}
