package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func postTranspose(t *testing.T, body string) (int, transposeResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	(&Server{}).handleTranspose(rec, httptest.NewRequest(http.MethodPost, "/api/transpose", strings.NewReader(body)))
	var resp transposeResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, resp
}

func TestHandleTranspose(t *testing.T) {
	text := `title: T\nkey: A\nchord: A X02220\n\n[V]\ngrid: | A | D | E7 |\n`

	code, a := postTranspose(t, `{"text":"`+text+`"}`)
	if code != http.StatusOK || a.Text != "" || a.Key != "A" || !reflect.DeepEqual(a.Missing, []string{"D", "E7"}) || !reflect.DeepEqual(a.DiagramKeys, []string{"A"}) {
		t.Errorf("analysis: %d %+v", code, a)
	}

	if len(a.Targets) != 11 || a.Targets[0] != (transposeTarget{Key: "E", Semitones: -5}) || a.Targets[3] != (transposeTarget{Key: "G", Semitones: -2}) || a.AllKeys != nil {
		t.Errorf("targets from A: %+v, all keys %v", a.Targets, a.AllKeys)
	}

	code, r := postTranspose(t, `{"text":"`+text+`","to":"G"}`)
	if code != http.StatusOK || r.From != "A" || r.To != "G" || r.Semitones != -2 || r.Kept != 1 {
		t.Fatalf("transpose: %d %+v", code, r)
	}
	if !strings.Contains(r.Text, "key: G\n") || !strings.Contains(r.Text, "chord[A]: A X02220\n") || !strings.Contains(r.Text, "grid: | G | C | D7 |") {
		t.Errorf("text:\n%s", r.Text)
	}
	if !reflect.DeepEqual(r.Missing, []string{"G", "C", "D7"}) || !reflect.DeepEqual(r.DiagramKeys, []string{"A"}) {
		t.Errorf("after transpose: missing %v, diagram keys %v", r.Missing, r.DiagramKeys)
	}

	if code, _ := postTranspose(t, `{"text":"`+text+`","to":"Em"}`); code != http.StatusBadRequest {
		t.Errorf("major to minor: %d, want 400", code)
	}
}
