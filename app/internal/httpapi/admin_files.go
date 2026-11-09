package httpapi

import (
	"net/http"
	"path/filepath"
)

// handleAdminFile is GET /admin/files/{name} — serves a file from
// adminFileDir (see Server.adminFileDir's own doc comment), 404 if the
// route is disabled (adminFileDir empty) or the name doesn't match an
// entry in that directory. filepath.Base strips any path separators
// from the URL's {name} segment before joining, so a request can't
// escape adminFileDir no matter what it contains — there is deliberately
// no subdirectory support here, this is a flat staging area only.
func (s *Server) handleAdminFile(w http.ResponseWriter, r *http.Request) {
	if s.adminFileDir == "" {
		http.NotFound(w, r)
		return
	}
	name := filepath.Base(r.PathValue("name"))
	http.ServeFile(w, r, filepath.Join(s.adminFileDir, name))
}
