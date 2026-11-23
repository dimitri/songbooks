package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/soundfont"
)

func TestSoundFontSubsetEndpoint(t *testing.T) {
	old := SoundFonts
	t.Cleanup(func() { SoundFonts = old })
	get := func(file, query string, h http.Header) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/soundfonts/subset/"+file+query, nil)
		r.SetPathValue("file", file)
		for k, v := range h {
			r.Header[k] = v
		}
		w := httptest.NewRecorder()
		(&Server{}).handleSoundFontSubset(w, r)
		return w
	}

	// No bank: nothing to serve (the pages play with Sonivox).
	SoundFonts = nil
	if w := get("0-24.sf2", "", nil); w.Code != http.StatusNotFound {
		t.Fatalf("no bank: %d", w.Code)
	}

	// Sonivox stands in for GeneralUser GS here.
	SoundFonts = soundfont.NewSubsets("../webassets/static/js/vendor/alphatab/soundfont/sonivox.sf2", t.TempDir(), 1<<30)
	id := SoundFonts.ID()
	if id == "" {
		t.Fatal(SoundFonts.Err())
	}
	ok := "?sf=" + id

	w := get("0-24_0-33.sf2", ok, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("subset: %d %s", w.Code, w.Body)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control %q", cc)
	}
	etag := w.Header().Get("ETag")
	if etag == "" || !bytes.HasPrefix(w.Body.Bytes(), []byte("RIFF")) {
		t.Errorf("ETag %q, body %q…", etag, w.Body.Bytes()[:min(8, w.Body.Len())])
	}
	b, err := soundfont.Parse(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil || b.PresetCount() != 3 { // + the click
		t.Fatalf("served subset: %v, %d presets", err, b.PresetCount())
	}
	if w := get("0-24_0-33.sf2", ok, http.Header{"If-None-Match": {etag}}); w.Code != http.StatusNotModified {
		t.Errorf("If-None-Match: %d", w.Code)
	}

	// Another spelling, or no/another bank id: sent to the canonical URL.
	for _, c := range []struct{ file, query string }{{"0-33_0-24.sf2", ok}, {"0-24_0-33.sf2", ""}, {"0-24_0-33.sf2", "?sf=000000000000"}} {
		w := get(c.file, c.query, nil)
		if w.Code != http.StatusFound || w.Header().Get("Location") != "/soundfonts/subset/0-24_0-33.sf2"+ok {
			t.Errorf("%s%s: %d → %q", c.file, c.query, w.Code, w.Header().Get("Location"))
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("redirect Cache-Control %q", cc)
		}
	}

	// Bad keys.
	for _, file := range []string{"0-24", "0-128.sf2", "x.sf2", strings.Repeat("0-1_", 70) + "0-2.sf2"} {
		if w := get(file, ok, nil); w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", file, w.Code)
		}
	}
}
