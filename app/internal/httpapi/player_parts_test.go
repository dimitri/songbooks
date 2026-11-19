package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"app/internal/store"
)

// TestPlayerPartsAPI: the reader saves their own parts, for a band or a
// community they belong to, or elsewhere; not for one they don't; only
// instruments of the list. The song page's context decides which applies.
// Its own database (SONGBOOKS_HTTPAPI_TEST_DATABASE_URL, a scratch one:
// rows are added, never removed), skipped without it.
func TestPlayerPartsAPI(t *testing.T) {
	url := os.Getenv("SONGBOOKS_HTTPAPI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set SONGBOOKS_HTTPAPI_TEST_DATABASE_URL to a scratch database to run this")
	}
	ctx := context.Background()
	st, err := store.New(ctx, url)
	if err != nil {
		t.Skipf("no test database at %s: %v", url, err)
	}
	defer st.Close()
	sfx := fmt.Sprint(time.Now().UnixNano())
	person := func(name string, instruments ...string) *store.Person {
		id, err := st.CreatePerson(ctx, name+sfx, name, name+sfx+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if len(instruments) > 0 {
			if err := st.SetInstruments(ctx, id, instruments); err != nil {
				t.Fatal(err)
			}
		}
		p, err := st.GetPersonByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	mia, outsider := person("Mia", "guitarist", "singer"), person("Out")
	band, err := st.CreateBand(ctx, "Mercy "+sfx, mia.ID)
	if err != nil {
		t.Fatal(err)
	}
	jam, err := st.CreateCommunity(ctx, "Jam "+sfx, mia.ID)
	if err != nil {
		t.Fatal(err)
	}
	song, err := st.CreateVariant(ctx, store.OwnerPerson, mia.ID, "Song", "", mia.ID)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	put := func(as *store.Person, body string) (int, playerPartsJSON) {
		r := httptest.NewRequest("PUT", "/api/songs/x/parts", strings.NewReader(body))
		r.SetPathValue("id", fmt.Sprint(song))
		r = r.WithContext(context.WithValue(r.Context(), personCtxKey, as))
		w := httptest.NewRecorder()
		s.handlePutPlayerParts(w, r)
		var out playerPartsJSON
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, out
	}
	inBand := fmt.Sprintf(`"context":{"kind":"band","id":%d}`, band)
	inJam := fmt.Sprintf(`"context":{"kind":"community","id":%d}`, jam)

	// Guitar with the band, for all its songs.
	code, got := put(mia, `{`+inBand+`,"scope":"context","instruments":["guitar"]}`)
	if code != http.StatusOK || got.From != store.PartsFromContext || !slices.Equal(got.Parts, []string{"guitar"}) {
		t.Fatalf("the band: %d %+v", code, got)
	}
	// Voice and piano for this song in the jam.
	code, got = put(mia, `{`+inJam+`,"scope":"song","instruments":["keys","voice"]}`)
	if code != http.StatusOK || got.From != store.PartsFromSong || !slices.Equal(got.Parts, []string{"voice", "keys"}) {
		t.Fatalf("the song in the jam: %d %+v", code, got)
	}
	// Elsewhere: nothing set, the profile (normalised).
	code, got = put(mia, `{"context":{"kind":""},"scope":"song","clear":true}`)
	if code != http.StatusOK || got.From != store.PartsFromProfile || !slices.Equal(got.Parts, []string{"voice", "guitar"}) {
		t.Fatalf("elsewhere: %d %+v", code, got)
	}
	// Refused: a band one doesn't belong to, an unknown instrument or scope.
	for what, x := range map[string]struct {
		as   *store.Person
		body string
		want int
	}{
		"an outsider's band":    {outsider, `{` + inBand + `,"scope":"context","instruments":["voice"]}`, http.StatusForbidden},
		"an unknown instrument": {mia, `{` + inBand + `,"scope":"context","instruments":["kazoo"]}`, http.StatusBadRequest},
		"no scope":              {mia, `{` + inBand + `,"instruments":["voice"]}`, http.StatusBadRequest},
		"a school":              {mia, `{"context":{"kind":"school","id":1},"scope":"song","instruments":["voice"]}`, http.StatusBadRequest},
	} {
		if code, _ := put(x.as, x.body); code != x.want {
			t.Errorf("%s: %d, want %d", what, code, x.want)
		}
	}
	// Someone else's choice is theirs: the outsider, elsewhere, has the voice.
	if code, got := put(outsider, `{"context":{"kind":""},"scope":"context","clear":true}`); code != http.StatusOK || !slices.Equal(got.Parts, []string{"voice"}) || got.From != store.PartsFromDefault {
		t.Fatalf("the outsider: %d %+v", code, got)
	}

	// The page's context: the band's, the community's, or none.
	r := httptest.NewRequest("GET", "/song/x", nil).WithContext(context.WithValue(ctx, personCtxKey, mia))
	for what, x := range map[string]struct {
		data map[string]any
		want store.PartsContext
	}{
		"the band":      {map[string]any{"BandRef": band}, store.PartsContext{Kind: "band", ID: band}},
		"the community": {map[string]any{"CommunityRef": jam}, store.PartsContext{Kind: "community", ID: jam}},
		"nowhere":       {map[string]any{}, store.PartsContext{}},
	} {
		if c := s.partsContext(r, x.data); c != x.want {
			t.Errorf("the context from %s: %+v", what, c)
		}
	}
	var page playerPartsJSON
	if err := json.Unmarshal([]byte(s.playerPartsData(r, map[string]any{"BandRef": band}, song)), &page); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(page.Parts, []string{"guitar"}) || page.Context.Name != "Mercy "+sfx || len(page.All) == 0 {
		t.Fatalf("the page's parts: %+v", page)
	}
}
