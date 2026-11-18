package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChosenLang(t *testing.T) {
	r := httptest.NewRequest("GET", "/?lang=fr", nil)
	if l := chosenLang(r); l != "fr" {
		t.Fatalf("query: %q", l)
	}
	r = httptest.NewRequest("GET", "/?lang=de", nil)
	if l := chosenLang(r); l != "" {
		t.Fatalf("unknown language accepted: %q", l)
	}
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: langCookie, Value: "fr"})
	if l := chosenLang(r); l != "fr" {
		t.Fatalf("cookie: %q", l)
	}
	w := httptest.NewRecorder()
	rememberLang(w, httptest.NewRequest("GET", "/?lang=en", nil))
	if c := w.Result().Cookies(); len(c) != 1 || c[0].Name != langCookie || c[0].Value != "en" {
		t.Fatalf("not remembered: %v", c)
	}
}
