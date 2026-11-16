package httpapi

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"app/internal/store"
)

// A community's download area (docs/community-downloads.md): one page
// listing the files of every songbook of the community the viewer has
// access to — the book's PDF, the original Guitar Pro files, the backing
// tracks. A songbook is the community's unit of sale, so its access
// (store.CanAccessSongbook: open to every member, or sold separately and
// granted) decides who sees and downloads its files. Staff add and remove
// files here.

// downloadSongbook is one songbook's part of the download area.
type downloadSongbook struct {
	store.Songbook
	Books   []store.SongbookFile // the book itself: PDFs that go with no song
	Lessons []downloadLesson     // files that go with a song, or other loose files
	Songs   []store.SongbookEntry
	Files   int
	Bytes   int64
	Grants  int // staff: how many people hold a grant
}

// downloadLesson groups a song's files, or loose files with the same
// lesson number (or one loose file).
type downloadLesson struct {
	Number string // the author's lesson number, from the file names
	Title  string
	Href   string // the song, when the files go with one
	Files  []downloadFile
}

// downloadFile is a file of a lesson; Named when the lesson has another
// file of its kind, so the list shows which one it is.
type downloadFile struct {
	store.SongbookFile
	Named bool
}

// kindOrder is the order of a lesson's files: the score, then its audio.
var kindOrder = map[string]int{"gp": 0, "audio": 1, "other": 2, "book": 3}

// canSeeSongbook is CanAccessSongbook with the site admin bypass that
// requireCommunityMember already gives.
func (s *Server) canSeeSongbook(r *http.Request, sbID int64) (bool, error) {
	if personFromContext(r).IsAdmin {
		return true, nil
	}
	return s.store.CanAccessSongbook(r.Context(), personFromContext(r).ID, sbID)
}

// canSeeDefaultSongbook is canSeeSongbook for community communityID's
// default songbook, which has its own page and PDF export.
func (s *Server) canSeeDefaultSongbook(r *http.Request, communityID int64) (bool, error) {
	sbID, err := s.store.GetDefaultSongbook(r.Context(), "community", communityID)
	if err != nil {
		return false, err
	}
	return s.canSeeSongbook(r, sbID)
}

func (s *Server) isCommunityManager(r *http.Request, communityID int64) (bool, error) {
	person := personFromContext(r)
	if person.IsAdmin {
		return true, nil
	}
	return s.store.HasAnyCommunityManagePrivilege(r.Context(), communityID, person.ID)
}

// handleCommunityDownloadsPage is GET /communities/{id}/downloads.
func (s *Server) handleCommunityDownloadsPage(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	community, err := s.store.GetCommunity(r.Context(), communityID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	isStaff, err := s.isCommunityManager(r, communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	songbooks, err := s.store.ListSongbooks(r.Context(), "community", communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var open []store.Songbook
	var ids []int64
	for _, sb := range songbooks {
		ok, err := s.canSeeSongbook(r, sb.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ok {
			open = append(open, sb)
			ids = append(ids, sb.ID)
		}
	}
	files, err := s.store.ListSongbookFiles(r.Context(), ids)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	bySongbook := map[int64][]store.SongbookFile{}
	for _, f := range files {
		bySongbook[f.SongbookID] = append(bySongbook[f.SongbookID], f)
	}
	var parts []downloadSongbook
	var total int
	for _, sb := range open {
		part := downloadSongbook{Songbook: sb}
		lessonAt := map[string]int{}
		for _, f := range bySongbook[sb.ID] {
			part.Bytes += f.ByteSize
			part.Files++
			total++
			if f.VariantID == nil && f.Kind == "book" {
				part.Books = append(part.Books, f)
				continue
			}
			key, l := "", downloadLesson{Number: f.Number(), Title: f.Label()}
			switch {
			case f.VariantID != nil:
				key = fmt.Sprintf("song:%d", *f.VariantID)
				l.Title, l.Href = f.SongTitle, fmt.Sprintf("/song/%d", *f.VariantID)
			case l.Number != "":
				key = "lesson:" + l.Number
			}
			i, seen := lessonAt[key]
			if seen && key != "" && len(l.Title) < len(part.Lessons[i].Title) && f.VariantID == nil {
				part.Lessons[i].Title = l.Title // "Same old Blues", not "Same old Blues PB"
			}
			if !seen || key == "" {
				i = len(part.Lessons)
				lessonAt[key] = i
				part.Lessons = append(part.Lessons, l)
			}
			part.Lessons[i].Files = append(part.Lessons[i].Files, downloadFile{SongbookFile: f})
		}
		for _, l := range part.Lessons {
			sort.SliceStable(l.Files, func(i, j int) bool { return kindOrder[l.Files[i].Kind] < kindOrder[l.Files[j].Kind] })
			kinds := map[string]int{}
			for _, f := range l.Files {
				kinds[f.Kind]++
			}
			for i := range l.Files {
				l.Files[i].Named = kinds[l.Files[i].Kind] > 1
			}
		}
		// By lesson number, then title: "01_ Mon premier Blues 2" after "01_Mon premier Blues 1".
		sort.SliceStable(part.Lessons, func(i, j int) bool {
			a, _ := strconv.Atoi(part.Lessons[i].Number)
			b, _ := strconv.Atoi(part.Lessons[j].Number)
			if a != b {
				return a < b
			}
			return part.Lessons[i].Title < part.Lessons[j].Title
		})
		if isStaff {
			// What a new file can go with, and who holds the songbook.
			if part.Songs, err = s.store.ListSongbookEntries(r.Context(), sb.ID); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			grants, err := s.store.ListSongbookAccessGrants(r.Context(), sb.ID)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, g := range grants {
				if !g.Expired() {
					part.Grants++
				}
			}
		} else if len(bySongbook[sb.ID]) == 0 {
			continue // nothing to download from it
		}
		parts = append(parts, part)
	}
	s.render(w, "community_downloads", map[string]any{
		"Me": person, "Community": community, "Songbooks": parts, "IsStaff": isStaff, "FileCount": total,
	})
}

// songbookFileFor resolves {fileID} to a file of a songbook of community {id}.
func (s *Server) songbookFileFor(r *http.Request) (store.SongbookFile, *store.Songbook, error) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	fileID, err := pathID(r, "fileID")
	if err != nil {
		return store.SongbookFile{}, nil, store.ErrNotFound
	}
	f, err := s.store.GetSongbookFile(r.Context(), fileID)
	if err != nil {
		return f, nil, err
	}
	sb, err := s.store.GetSongbook(r.Context(), f.SongbookID)
	if err != nil {
		return f, sb, err
	}
	if sb.OwnerKind != "community" || sb.OwnerCommunityID != communityID {
		return f, sb, store.ErrNotFound
	}
	return f, sb, nil
}

// handleDownloadSongbookFile is GET /communities/{id}/downloads/{fileID}/{name}:
// the file itself, to anyone with access to its songbook.
func (s *Server) handleDownloadSongbookFile(w http.ResponseWriter, r *http.Request) {
	f, sb, err := s.songbookFileFor(r)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ok, err := s.canSeeSongbook(r, sb.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	} else if !ok {
		// Same answer as a file that isn't there: what a songbook holds is
		// for the people who have it.
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", f.ContentType)
	disposition := "attachment"
	if r.URL.Query().Get("view") == "1" {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": f.Filename}))
	w.Header().Set("ETag", `"`+f.SHA256+`"`)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", f.CreatedAt, s.store.OpenSongbookFile(r.Context(), f))
}

// handleUploadSongbookFiles is POST /api/communities/{id}/songbooks/{sbID}/files,
// staff only: a multipart form with fields "song" (a song of the songbook
// the files go with, optional), "title" (optional, one file) and one or
// more "file" parts, read as they arrive.
func (s *Server) handleUploadSongbookFiles(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	sbID, err := pathID(r, "sbID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid songbook id")
		return
	}
	sb, err := s.store.GetSongbook(r.Context(), sbID)
	if err != nil || sb.OwnerKind != "community" || sb.OwnerCommunityID != communityID {
		httpError(w, http.StatusNotFound, "no such songbook")
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		httpError(w, http.StatusBadRequest, "expected a multipart form: "+err.Error())
		return
	}
	var variantID *int64
	var title string
	type added struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Existed bool   `json:"existed"`
	}
	var out []added
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		switch part.FormName() {
		case "song", "title":
			v, _ := io.ReadAll(io.LimitReader(part, 1024))
			if part.FormName() == "title" {
				title = strings.TrimSpace(string(v))
				break
			}
			if str := strings.TrimSpace(string(v)); str != "" {
				id, err := strconv.ParseInt(str, 10, 64)
				if err != nil {
					httpError(w, http.StatusBadRequest, "invalid song")
					return
				}
				in, err := s.store.SongbookHasVariant(r.Context(), sbID, id)
				if err != nil || !in {
					httpError(w, http.StatusBadRequest, "that song isn't in this songbook")
					return
				}
				variantID = &id
			}
		case "file":
			name := filepath.Base(part.FileName())
			if name == "" || name == "." {
				httpError(w, http.StatusBadRequest, "a file part has no file name")
				return
			}
			id, existed, err := s.store.AddSongbookFile(r.Context(), sbID, variantID, store.FileKind(name, variantID != nil), name, title,
				store.FileContentType(name), part, personFromContext(r).ID)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			out = append(out, added{ID: id, Name: name, Existed: existed})
		}
	}
	if len(out) == 0 {
		httpError(w, http.StatusBadRequest, "no file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

// handleDeleteSongbookFile is DELETE /api/communities/{id}/downloads/{fileID}, staff only.
func (s *Server) handleDeleteSongbookFile(w http.ResponseWriter, r *http.Request) {
	f, _, err := s.songbookFileFor(r)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such file")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.DeleteSongbookFile(r.Context(), f.ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
