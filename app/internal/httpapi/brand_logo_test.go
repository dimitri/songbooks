package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"app/internal/store"
)

// testStore opens the test database (as internal/store's tests do, and
// under the same lock: one test at a time across packages), emptied;
// skipped without one.
func testStore(t *testing.T) *store.Store {
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

func logoUpload(t *testing.T, name string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(data)
	mw.Close()
	return &body, mw.FormDataContentType()
}

// TestBrandLogoUpload: who may save a community's or band's look may
// upload and remove its logo, nobody else; the logo is then served to
// anyone at its own address, cached for good.
func TestBrandLogoUpload(t *testing.T) {
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
	staff, member, outsider, core, guest := person("staff"), person("member"), person("outsider"), person("core"), person("guest")
	cid, err := st.CreateCommunity(ctx, "Studio", staff.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddCommunityMember(ctx, cid, member.ID, "member", staff.ID); err != nil {
		t.Fatal(err)
	}
	bid, err := st.CreateBand(ctx, "Band", core.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBandMember(ctx, bid, guest.ID, "guest", core.ID); err != nil {
		t.Fatal(err)
	}

	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := 8; i < 56; i++ {
		img.Set(i, i, color.NRGBA{200, 40, 90, 255})
	}
	var pngData bytes.Buffer
	png.Encode(&pngData, img)

	as := func(p *store.Person, method, path string, file []byte, name string) *httptest.ResponseRecorder {
		t.Helper()
		h := NewServer(st, nil, p, "", "")
		var r *http.Request
		if file != nil {
			body, ct := logoUpload(t, name, file)
			r = httptest.NewRequest(method, path, body)
			r.Header.Set("Content-Type", ct)
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	community := "/api/communities/" + itoa(cid) + "/brand/logo"
	band := "/api/bands/" + itoa(bid) + "/brand/logo"

	for _, c := range []struct {
		who    *store.Person
		method string
		path   string
	}{
		{member, "PUT", community}, {outsider, "PUT", community}, {member, "DELETE", community},
		{guest, "PUT", band}, {outsider, "PUT", band}, {guest, "DELETE", band},
	} {
		var file []byte
		if c.method == "PUT" {
			file = pngData.Bytes()
		}
		if w := as(c.who, c.method, c.path, file, "logo.png"); w.Code != http.StatusForbidden {
			t.Errorf("%s %s by %s: %d, want 403", c.method, c.path, c.who.DisplayName, w.Code)
		}
	}
	if w := as(nil, "PUT", community, pngData.Bytes(), "logo.png"); w.Code != http.StatusUnauthorized && w.Code != http.StatusSeeOther && w.Code != http.StatusFound {
		t.Errorf("PUT without a login: %d", w.Code)
	}

	if w := as(staff, "PUT", community, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), "logo.svg"); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("an SVG: %d, want 415", w.Code)
	}

	w := as(staff, "PUT", community, pngData.Bytes(), "logo.png")
	if w.Code != http.StatusOK {
		t.Fatalf("PUT by staff: %d %s", w.Code, w.Body)
	}
	var got struct {
		LogoImage string `json:"logoImage"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if !strings.HasPrefix(got.LogoImage, "/brand-logos/community/"+itoa(cid)+"/") {
		t.Fatalf("logoImage %q", got.LogoImage)
	}
	if w := as(core, "PUT", band, pngData.Bytes(), "logo.png"); w.Code != http.StatusOK {
		t.Fatalf("PUT by a core member: %d %s", w.Code, w.Body)
	}

	// Served without a login, cached for good, revalidated by its ETag.
	w = as(nil, "GET", got.LogoImage, nil, "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" ||
		!strings.Contains(w.Header().Get("Cache-Control"), "immutable") || w.Header().Get("ETag") == "" {
		t.Fatalf("GET logo: %d %v", w.Code, w.Header())
	}
	if _, err := png.Decode(w.Body); err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	h := NewServer(st, nil, nil, "", "")
	r := httptest.NewRequest("GET", got.LogoImage, nil)
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w304 := httptest.NewRecorder()
	h.ServeHTTP(w304, r)
	if w304.Code != http.StatusNotModified {
		t.Errorf("If-None-Match: %d, want 304", w304.Code)
	}
	if w := as(nil, "GET", "/brand-logos/community/"+itoa(cid)+"/0000000000000000.png", nil, ""); w.Code != http.StatusNotFound {
		t.Errorf("another hash: %d, want 404", w.Code)
	}

	// The brand form can't point the logo elsewhere; its pages show it.
	staffServer := NewServer(st, nil, staff, "", "")
	r = httptest.NewRequest("POST", "/api/communities/"+itoa(cid)+"/brand", strings.NewReader(`{"mark":"S","logoImage":"/brand-logos/band/`+itoa(bid)+`/0000000000000000.png"}`))
	w = httptest.NewRecorder()
	staffServer.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), got.LogoImage) {
		t.Fatalf("POST brand: %d %s", w.Code, w.Body)
	}

	if w := as(staff, "DELETE", community, nil, ""); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "logoImage") {
		t.Fatalf("DELETE by staff: %d %s", w.Code, w.Body)
	}
	if w := as(nil, "GET", got.LogoImage, nil, ""); w.Code != http.StatusNotFound {
		t.Errorf("a removed logo: %d, want 404", w.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
