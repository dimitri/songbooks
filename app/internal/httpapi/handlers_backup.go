package httpapi

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"app/internal/setlist"
)

type backupManifest struct {
	Name       string `json:"name"`
	ExportedAt string `json:"exportedAt"`
}

// handleExportBook streams a whole project (every song and setlist row —
// scores/assets aren't database-backed yet, so a zip exported today omits
// them, unlike the old filesystem version) as a single downloadable .zip,
// plus a small manifest.json at the root so an import can sanity-check what
// it's unpacking before touching the database.
func (s *Server) handleExportBook(w http.ResponseWriter, r *http.Request) {
	book := r.PathValue("book")
	songs, err := s.store.ListSongs(book)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such project")
		return
	}
	setlists, _ := s.store.ListSetlists(book)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, book))

	zw := zip.NewWriter(w)
	defer zw.Close()

	manifest, _ := json.MarshalIndent(backupManifest{Name: book, ExportedAt: time.Now().Format(time.RFC3339)}, "", "  ")
	if mw, err := zw.Create("songbooks-manifest.json"); err == nil {
		mw.Write(manifest)
	}

	for _, stem := range songs {
		text, _, err := s.store.LoadRaw(book, stem)
		if err != nil {
			continue
		}
		if fw, err := zw.Create("songs/" + stem + ".song"); err == nil {
			io.WriteString(fw, text)
		}
	}
	for _, stem := range setlists {
		l, _, err := s.store.LoadSetlist(book, stem)
		if err != nil {
			continue
		}
		if fw, err := zw.Create("setlists/" + stem + ".setlist"); err == nil {
			io.WriteString(fw, setlist.Write(l))
		}
	}
}

// handleImportBook creates a NEW project from a previously exported zip —
// deliberately never overwrites an existing one (a mismatched re-import
// clobbering real edits is exactly the kind of silent data loss this app
// is supposed to prevent); importing into an existing name fails and the
// caller picks a different one or deletes the old project first.
func (s *Server) handleImportBook(w http.ResponseWriter, r *http.Request) {
	book := slugify(r.FormValue("book"))
	if book == "" {
		httpError(w, http.StatusBadRequest, "missing project name")
		return
	}
	if existing, _ := s.store.ListBooks(); contains(existing, book) {
		httpError(w, http.StatusConflict, "a project named \""+book+"\" already exists")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		httpError(w, http.StatusBadRequest, "missing file")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	zr, err := zip.NewReader(strings.NewReader(string(data)), int64(len(data)))
	if err != nil {
		httpError(w, http.StatusBadRequest, "not a valid zip file")
		return
	}

	if err := s.store.CreateBook(book); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p := personFromContext(r); p != nil {
		if err := s.store.AddMember(r.Context(), book, p.ID, "owner"); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	for _, f := range zr.File {
		switch {
		case f.Name == "songbooks-manifest.json":
			continue
		case strings.HasPrefix(f.Name, "songs/") && strings.HasSuffix(f.Name, ".song") && !f.FileInfo().IsDir():
			stem := strings.TrimSuffix(strings.TrimPrefix(f.Name, "songs/"), ".song")
			if strings.ContainsAny(stem, "/\\") || stem == "" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				continue
			}
			body, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			s.store.ImportSong(book, stem, string(body))
		case strings.HasPrefix(f.Name, "setlists/") && strings.HasSuffix(f.Name, ".setlist") && !f.FileInfo().IsDir():
			stem := strings.TrimSuffix(strings.TrimPrefix(f.Name, "setlists/"), ".setlist")
			if strings.ContainsAny(stem, "/\\") || stem == "" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				continue
			}
			body, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			s.store.ImportSetlist(book, stem, string(body))
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"book": book, "name": header.Filename})
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
