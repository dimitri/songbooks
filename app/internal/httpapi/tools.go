package httpapi

import "net/http"

// handleToolsPage is GET /tools: the phone's Tools tab (menu PR 9), the
// practice tools that aren't a place of their own — the metronome and the
// tuner. The desktop sidebar lists them directly.
func (s *Server) handleToolsPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "tools", map[string]any{"Me": personFromContext(r)})
}
