package httpapi

import (
	"net/http/httptest"
	"testing"
)

// A set list export prints the Song layout in two columns unless the
// link asks for one (?cols=1); out-of-range values fall back to the
// default rather than reaching the renderer.
func TestSetlistOptionsColumns(t *testing.T) {
	for q, want := range map[string]int{"": 2, "?cols=1": 1, "?cols=2": 2, "?cols=3": 2, "?cols=x": 2, "?mode=lyrics": 2} {
		r := httptest.NewRequest("GET", "/setlists/1/export.pdf"+q, nil)
		if got := setlistOptionsFromRequest(r).Cols(); got != want {
			t.Errorf("%q: %d columns, want %d", q, got, want)
		}
	}
}
