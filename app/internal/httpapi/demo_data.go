package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/jackc/pgx/v5"
)

// Demo data (cmd/sbdemo, docs/demo-data.md) from the admin page, on a
// deployment that says it's a demo copy (SONGBOOKS_DEMO_DATA=1: the local
// stack, staging) and nowhere else: production has no such panel, and
// sbdemo refuses production on its own. The buttons run the sbdemo binary
// shipped in the image: anonymize the copy, add the fixtures, remove them.

// DemoData is set by main from SONGBOOKS_DEMO_DATA; DemoDBURL is the
// database the app (and so sbdemo) works on.
var (
	DemoData  bool
	DemoDBURL string
)

// demoStatus is what the admin page says: how many rows the fixtures
// made, and whether the copy looks anonymized.
func (s *Server) demoStatus(ctx context.Context) map[string]any {
	if !DemoData {
		return nil
	}
	rows, anonymized, err := s.store.DemoStatus(ctx)
	out := map[string]any{"Rows": int(rows), "Anonymized": anonymized}
	if err != nil {
		out["Error"] = err.Error()
	}
	return out
}

// handleAdminDemo is POST /api/admin/demo/{action}: anonymize, fixtures
// or remove, by the sbdemo binary, on this deployment's database; its
// counts back (table by table, never a name).
func (s *Server) handleAdminDemo(w http.ResponseWriter, r *http.Request) {
	if !DemoData {
		http.NotFound(w, r)
		return
	}
	action := r.PathValue("action")
	if action != "anonymize" && action != "fixtures" && action != "remove" {
		http.NotFound(w, r)
		return
	}
	cfg, err := pgx.ParseConfig(DemoDBURL)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "reading the database address: "+err.Error())
		return
	}
	confirm := cfg.Database + "@" + cfg.Host
	// anonymize rewrites every name: the page asks for the copy's name
	// back, as sbdemo itself does.
	if action == "anonymize" {
		var req struct {
			Confirm string `json:"confirm"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Confirm != confirm {
			httpError(w, http.StatusBadRequest, "type "+confirm+" to anonymize this copy")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sbdemo", action, "--db", DemoDBURL, "--confirm", confirm)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "output": string(out) + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": string(out)})
}
