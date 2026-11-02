package httpapi

import "net/http"

// handleDevOutbox lists the most recent emails the console mailer has
// "sent" (see internal/mailer.ConsoleMailer), so a magic login link can
// be fetched from a browser instead of tailing server stdout. Gated by
// requireAdmin (router.go) — it contains live, single-use login links,
// so it must not be publicly reachable.
func (s *Server) handleDevOutbox(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.ListOutbox(r.Context(), 50)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "dev_outbox", map[string]any{"Me": personFromContext(r), "Entries": entries})
}
