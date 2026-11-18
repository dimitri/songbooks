package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// All lyrics below are invented placeholder lines.

func TestHandleImportMerge(t *testing.T) {
	current := "title: Quiet Road\nmap: Intro · Verse\n\n[Intro]\ngrid: | A | D |\n\n[Verse]\n"
	draft := "title: Quiet Road\ntempo: 100\n\n[Verse]\ngrid: | A | E |\nwalking down the quiet road\n"
	call := func(mode string) (int, string, []string) {
		body, _ := json.Marshal(map[string]string{"currentText": current, "draft": draft, "mode": mode})
		w := httptest.NewRecorder()
		(&Server{}).handleImportMerge(w, httptest.NewRequest(http.MethodPost, "/api/import/merge", strings.NewReader(string(body))))
		var res struct {
			Text  string   `json:"text"`
			Notes []string `json:"notes"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		return w.Code, res.Text, res.Notes
	}
	code, text, notes := call("merge")
	if code != http.StatusOK || !strings.Contains(text, "[Intro]\ngrid: | A | D |\n\n[Verse]\ngrid: | A | E |\nwalking down the quiet road") || !strings.Contains(text, "tempo: 100") || len(notes) == 0 {
		t.Errorf("merge: %d %q %q", code, text, notes)
	}
	if _, text, _ = call("replace"); strings.Contains(text, "[Intro]") || !strings.Contains(text, "map: Intro · Verse") {
		t.Errorf("replace: %q", text)
	}
	if _, text, _ = call("append"); text != current+"\n\n"+draft {
		t.Errorf("append: %q", text)
	}
	if code, _, _ = call("bogus"); code != http.StatusBadRequest {
		t.Errorf("bogus mode: %d", code)
	}
}
