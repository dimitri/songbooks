package httpapi

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"app/internal/store"
)

// A community's download area (docs/community-downloads.md): one page
// listing the files of every songbook of the community the viewer has
// access to — the book's PDF, the original Guitar Pro files, the backing
// tracks — lesson by lesson, each songbook also as one zip, or folder by
// folder. A songbook is the community's unit of sale, so its access
// (store.CanAccessSongbook: open to every member, or sold separately and
// granted) decides whose files they are; a role's download_files privilege
// whether they may download them at all; manage_files who adds, moves and
// removes them.

// downloadSongbook is one songbook's part of the download area.
type downloadSongbook struct {
	store.Songbook
	Books   []store.SongbookFile // the book itself: PDFs that go with no song
	Lessons []downloadLesson     // files that go with a song, or other loose files
	Songs   []store.SongbookEntry
	Files   int
	Bytes   int64
	Folders bool // some files are in folders
	Grants  int  // staff: how many people hold a grant
	ZipHref string
	Folder  string // where files added go
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

// communityPrivilege is HasCommunityPrivilege with the site admin bypass.
func (s *Server) communityPrivilege(r *http.Request, communityID int64, priv string) (bool, error) {
	person := personFromContext(r)
	if person.IsAdmin {
		return true, nil
	}
	return s.store.HasCommunityPrivilege(r.Context(), communityID, person.ID, priv)
}

// canDownload: a role that may download, or that manages the files.
func (s *Server) canDownload(r *http.Request, communityID int64) (bool, error) {
	ok, err := s.communityPrivilege(r, communityID, store.PrivDownloadFiles)
	if err != nil || ok {
		return ok, err
	}
	return s.communityPrivilege(r, communityID, store.PrivManageFiles)
}

// browseHref is a folder of a songbook's files, "" for the top.
func browseHref(communityID, sbID int64, folder string) string {
	h := fmt.Sprintf("/communities/%d/downloads/songbooks/%d/browse/", communityID, sbID)
	if folder == "" {
		return h
	}
	var parts []string
	for _, p := range strings.Split(folder, "/") {
		parts = append(parts, url.PathEscape(p))
	}
	return h + strings.Join(parts, "/") + "/"
}

// zipName is what the zip of a songbook's folder is called.
func zipName(sb *store.Songbook, folder string) string {
	name := sb.Name
	if folder != "" {
		name = path.Base(folder)
	}
	return strings.NewReplacer("/", "-", "\\", "-").Replace(name) + ".zip"
}

// zipHref is the zip of a folder of a songbook's files, "" for all.
func zipHref(communityID int64, sb *store.Songbook, folder string) string {
	h := fmt.Sprintf("/communities/%d/downloads/songbooks/%d/zip/%s", communityID, sb.ID, url.PathEscape(zipName(sb, folder)))
	if folder != "" {
		h += "?path=" + url.QueryEscape(folder)
	}
	return h
}

// handleCommunityDownloadsPage is GET /communities/{id}/downloads: each
// songbook or course the member has, with its main downloads (its books,
// everything as a zip); and GET /communities/{id}/downloads/songbooks/{sbID}:
// one of them, lesson by lesson, every file.
func (s *Server) handleCommunityDownloadsPage(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	only, _ := strconv.ParseInt(r.PathValue("sbID"), 10, 64)
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
	canManage, err := s.communityPrivilege(r, communityID, store.PrivManageFiles)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	canDownload, err := s.canDownload(r, communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := map[string]any{
		"Me": person, "Community": community, "IsStaff": isStaff, "CanManage": canManage, "CanDownload": canDownload,
		"One": only != 0,
	}
	if !canDownload {
		s.render(w, "community_downloads", data)
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
		if only != 0 && sb.ID != only {
			continue
		}
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
	for _, sb := range open {
		part := downloadSongbook{Songbook: sb, ZipHref: zipHref(communityID, &sb, "")}
		lessonAt := map[string]int{}
		// A course's files are listed by its lessons.
		lessons := map[int64]store.Lesson{}
		if sb.IsCourse() {
			ls, _, err := s.store.ListLessons(r.Context(), sb.ID)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, l := range ls {
				lessons[l.ID] = l
			}
		}
		for _, f := range bySongbook[sb.ID] {
			part.Bytes += f.ByteSize
			part.Files++
			part.Folders = part.Folders || f.Path != ""
			if f.VariantID == nil && f.Kind == "book" {
				part.Books = append(part.Books, f)
				continue
			}
			key, l := "", downloadLesson{Number: f.Number(), Title: f.Label()}
			lesson, inLesson := store.Lesson{}, false
			if f.LessonID != nil {
				lesson, inLesson = lessons[*f.LessonID]
			}
			switch {
			case inLesson:
				key = fmt.Sprintf("lessonid:%d", lesson.ID)
				l.Number, l.Title, l.Href = "", lesson.Title, lessonHref(sb.ID, lesson.ID)
				if lesson.Number != nil {
					l.Number = strconv.Itoa(*lesson.Number)
				}
			case f.VariantID != nil:
				key = fmt.Sprintf("song:%d", *f.VariantID)
				l.Title, l.Href = f.SongTitle, fmt.Sprintf("/song/%d", *f.VariantID)
			case l.Number != "":
				key = "lesson:" + l.Number
			}
			i, seen := lessonAt[key]
			if seen && key != "" && len(l.Title) < len(part.Lessons[i].Title) && f.VariantID == nil && !inLesson {
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
		if canManage {
			// What a new file can go with.
			if part.Songs, err = s.store.ListSongbookEntries(r.Context(), sb.ID); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			sortEntriesByTitle(part.Songs)
		}
		if isStaff {
			// Who holds the songbook.
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
	if only != 0 && len(parts) == 0 {
		http.NotFound(w, r)
		return
	}
	data["Songbooks"] = parts
	s.render(w, "community_downloads", data)
}

func sortEntriesByTitle(entries []store.SongbookEntry) {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Title < entries[j].Title })
}

// songbookFor resolves {sbID} to a songbook of community {id}.
func (s *Server) songbookFor(r *http.Request) (*store.Songbook, error) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	sbID, err := pathID(r, "sbID")
	if err != nil {
		return nil, store.ErrNotFound
	}
	sb, err := s.store.GetSongbook(r.Context(), sbID)
	if err != nil {
		return nil, err
	}
	if sb.OwnerKind != "community" || sb.OwnerCommunityID != communityID {
		return nil, store.ErrNotFound
	}
	return sb, nil
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

// mayDownloadFrom: the songbook is the reader's, and their role downloads.
// Not found otherwise — what a songbook holds is for the people who have
// it.
func (s *Server) mayDownloadFrom(w http.ResponseWriter, r *http.Request, sb *store.Songbook) bool {
	ok, err := s.canSeeSongbook(r, sb.ID)
	if err == nil && ok {
		ok, err = s.canDownload(r, sb.OwnerCommunityID)
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !ok {
		http.NotFound(w, r)
		return false
	}
	return true
}

// startsDownload: a request for the whole file, or its start — a resumed
// download or a player's seek isn't another download.
func startsDownload(r *http.Request) bool {
	rng := r.Header.Get("Range")
	return rng == "" || rng == "bytes=0-"
}

// handleDownloadSongbookFile is GET /communities/{id}/downloads/{fileID}/{name}:
// the file itself, to anyone with access to its songbook and a role that
// downloads. ?view=1 opens it in the browser (a PDF, a backing track to
// play).
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
	if !s.mayDownloadFrom(w, r, sb) {
		return
	}
	disposition, what := "attachment", "download"
	if r.URL.Query().Get("view") == "1" {
		disposition, what = "inline", "open"
		if f.Kind == "audio" {
			what = "play"
		}
	}
	if startsDownload(r) && r.Method == http.MethodGet {
		if err := s.store.RecordSongbookDownload(r.Context(), sb.OwnerCommunityID, sb.ID, &f.ID, personFromContext(r).ID, what, f.FullPath(), f.ByteSize); err != nil {
			log.Printf("recording download of file %d: %v", f.ID, err)
		}
	}
	w.Header().Set("Content-Type", f.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": f.Filename}))
	w.Header().Set("ETag", `"`+f.SHA256+`"`)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", f.ModifiedAt, s.store.OpenSongbookFile(r.Context(), f))
}

// filesUnder is the files of folder, and of its folders.
func filesUnder(files []store.SongbookFile, folder string) []store.SongbookFile {
	if folder == "" {
		return files
	}
	var out []store.SongbookFile
	for _, f := range files {
		if f.Path == folder || strings.HasPrefix(f.Path, folder+"/") {
			out = append(out, f)
		}
	}
	return out
}

// handleDownloadSongbookZip is GET /communities/{id}/downloads/songbooks/{sbID}/zip/{name}:
// every file of the songbook as one zip, or of its folder ?path=, made as
// it's sent (it resumes, too).
func (s *Server) handleDownloadSongbookZip(w http.ResponseWriter, r *http.Request) {
	sb, err := s.songbookFor(r)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.mayDownloadFrom(w, r, sb) {
		return
	}
	folder := store.CleanFilePath(r.URL.Query().Get("path"))
	all, err := s.store.ListSongbookFiles(r.Context(), []int64{sb.ID})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	files := filesUnder(all, folder)
	if len(files) == 0 {
		http.NotFound(w, r)
		return
	}
	zs, err := s.store.NewZipStream(r.Context(), files, folder)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if startsDownload(r) && r.Method == http.MethodGet {
		name := sb.Name
		if folder != "" {
			name += " / " + folder
		}
		if err := s.store.RecordSongbookDownload(r.Context(), sb.OwnerCommunityID, sb.ID, nil, personFromContext(r).ID, "zip", name, zs.Size()); err != nil {
			log.Printf("recording zip of songbook %d: %v", sb.ID, err)
		}
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": zipName(sb, folder)}))
	w.Header().Set("ETag", zs.ETag)
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, "", zs.Modified, zs)
}

// browseFolder is a folder in a folder of a songbook's files.
type browseFolder struct {
	Name  string
	Href  string
	Files int
	Bytes int64
}

// browseCrumb is a folder on the way to the one shown.
type browseCrumb struct {
	Name, Href string
	Current    bool
}

// browseFile is a file of the folder shown; SongID the song it goes
// with, 0 for none.
type browseFile struct {
	store.SongbookFile
	SongID int64
}

// handleBrowseSongbookFiles is GET /communities/{id}/downloads/songbooks/{sbID}/browse/{path...}:
// a folder of a songbook's files, as in its zip — its folders, its files.
func (s *Server) handleBrowseSongbookFiles(w http.ResponseWriter, r *http.Request) {
	communityID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	sb, err := s.songbookFor(r)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.mayDownloadFrom(w, r, sb) {
		return
	}
	community, err := s.store.GetCommunity(r.Context(), communityID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	canManage, err := s.communityPrivilege(r, communityID, store.PrivManageFiles)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	folder := store.CleanFilePath(r.PathValue("path"))
	all, err := s.store.ListSongbookFiles(r.Context(), []int64{sb.ID})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	under := filesUnder(all, folder)
	if folder != "" && len(under) == 0 {
		http.NotFound(w, r)
		return
	}
	var files []browseFile
	var folders []browseFolder
	at := map[string]int{}
	var bytes int64
	for _, f := range under {
		bytes += f.ByteSize
		if f.Path == folder {
			bf := browseFile{SongbookFile: f}
			if f.VariantID != nil {
				bf.SongID = *f.VariantID
			}
			files = append(files, bf)
			continue
		}
		rest := strings.TrimPrefix(f.Path, folder)
		name, _, _ := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
		i, ok := at[name]
		if !ok {
			i = len(folders)
			at[name] = i
			folders = append(folders, browseFolder{Name: name, Href: browseHref(communityID, sb.ID, path.Join(folder, name))})
		}
		folders[i].Files++
		folders[i].Bytes += f.ByteSize
	}
	crumbs := []browseCrumb{{Name: sb.Name, Href: browseHref(communityID, sb.ID, "")}}
	if folder != "" {
		parts := strings.Split(folder, "/")
		for i := range parts {
			crumbs = append(crumbs, browseCrumb{Name: parts[i], Href: browseHref(communityID, sb.ID, strings.Join(parts[:i+1], "/"))})
		}
	}
	crumbs[len(crumbs)-1].Current = true
	up := fmt.Sprintf("/communities/%d/downloads#songbook-%d", communityID, sb.ID)
	if len(crumbs) > 1 {
		up = crumbs[len(crumbs)-2].Href
	}
	data := map[string]any{
		"Me": personFromContext(r), "Community": community, "Songbook": sb, "Folder": folder,
		"Crumbs": crumbs, "Folders": folders, "Files": files, "Count": len(under), "Bytes": bytes,
		"ZipHref": zipHref(communityID, sb, folder), "ZipName": zipName(sb, folder), "CanManage": canManage, "UpHref": up,
	}
	if canManage {
		songs, err := s.store.ListSongbookEntries(r.Context(), sb.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sortEntriesByTitle(songs)
		data["Songs"] = songs
		data["Upload"] = downloadSongbook{Songbook: *sb, Songs: songs, Folder: folder}
	}
	s.render(w, "community_download_browse", data)
}

// handleUploadSongbookFiles is POST /api/communities/{id}/songbooks/{sbID}/files,
// for who manages the files: a multipart form with fields "path" (the
// folder they go in, optional), "song" (a song of the songbook they go
// with, optional), "title" (optional, one file) and one or more "file"
// parts. A zip is unpacked into the folder (unless "unzip" is "0"), its
// files matched to the songbook's songs (store.ImportSongbookZip).
func (s *Server) handleUploadSongbookFiles(w http.ResponseWriter, r *http.Request) {
	sb, err := s.songbookFor(r)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such songbook")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<30)
	mr, err := r.MultipartReader()
	if err != nil {
		httpError(w, http.StatusBadRequest, "expected a multipart form: "+err.Error())
		return
	}
	actor := personFromContext(r).ID
	var variantID *int64
	var title, folder string
	unzip := true
	var out []store.ImportedFile
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
		case "song", "title", "path", "unzip":
			v, _ := io.ReadAll(io.LimitReader(part, 1024))
			str := strings.TrimSpace(string(v))
			switch part.FormName() {
			case "title":
				title = str
			case "path":
				folder = store.CleanFilePath(str)
			case "unzip":
				unzip = str != "0"
			case "song":
				if str == "" {
					break
				}
				id, err := strconv.ParseInt(str, 10, 64)
				if err != nil {
					httpError(w, http.StatusBadRequest, "invalid song")
					return
				}
				in, err := s.store.SongbookHasVariant(r.Context(), sb.ID, id)
				if err != nil || !in {
					httpError(w, http.StatusBadRequest, "that song isn't in this songbook")
					return
				}
				variantID = &id
			}
		case "file":
			name := filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
			if name == "" || name == "." || name == "/" {
				httpError(w, http.StatusBadRequest, "a file part has no file name")
				return
			}
			if unzip && strings.EqualFold(filepath.Ext(name), ".zip") {
				res, err := s.importZipPart(r, sb.ID, part, name, folder, actor)
				if err != nil {
					httpError(w, http.StatusBadRequest, name+": "+err.Error())
					return
				}
				out = append(out, res...)
				continue
			}
			cr := &countingReader{r: part}
			id, status, err := s.store.AddSongbookFile(r.Context(), store.NewSongbookFile{
				SongbookID: sb.ID, VariantID: variantID, Path: folder, Filename: name, Title: title, CreatedByID: actor,
			}, cr)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			out = append(out, store.ImportedFile{Path: folder, Name: name, Size: cr.n, FileID: id, SongID: variantID, Status: status})
		}
	}
	if len(out) == 0 {
		httpError(w, http.StatusBadRequest, "no file")
		return
	}
	// A course's book, added on its own: its lessons' videos (a zip's
	// import does it itself).
	if sb.IsCourse() {
		for _, f := range out {
			if f.Status != "skipped" && f.SongID == nil && store.FileKind(f.Name, false) == "book" {
				if _, err := s.store.LessonVideosFromBooks(r.Context(), sb.ID); err != nil {
					log.Printf("songbook %d: lesson videos: %v", sb.ID, err)
				}
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// importZipPart unpacks an uploaded zip: kept on disk while it's read (a
// zip is read from its end).
func (s *Server) importZipPart(r *http.Request, sbID int64, part io.Reader, name, folder string, actor int64) ([]store.ImportedFile, error) {
	tmp, err := os.CreateTemp("", "songbook-upload-*.zip")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	size, err := io.Copy(tmp, part)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return nil, fmt.Errorf("not a zip file: %w", err)
	}
	return s.store.ImportSongbookZip(r.Context(), sbID, zr, name, folder, actor)
}

// handleSetSongbookFileSong is PATCH /api/communities/{id}/downloads/{fileID},
// for who manages the files: {"song": id} says which song of its songbook
// it goes with, {"song": null} none.
func (s *Server) handleSetSongbookFileSong(w http.ResponseWriter, r *http.Request) {
	f, sb, err := s.songbookFileFor(r)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such file")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var req struct {
		Song *int64 `json:"song"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Song != nil {
		in, err := s.store.SongbookHasVariant(r.Context(), sb.ID, *req.Song)
		if err != nil || !in {
			httpError(w, http.StatusBadRequest, "that song isn't in this songbook")
			return
		}
	}
	if err := s.store.SetSongbookFileSong(r.Context(), f.ID, req.Song); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// handleDeleteSongbookFile is DELETE /api/communities/{id}/downloads/{fileID},
// for who manages the files.
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

// handleDeleteSongbookFolder is DELETE /api/communities/{id}/songbooks/{sbID}/files?path=folder,
// for who manages the files: a folder and everything in it.
func (s *Server) handleDeleteSongbookFolder(w http.ResponseWriter, r *http.Request) {
	sb, err := s.songbookFor(r)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such songbook")
		return
	}
	folder := store.CleanFilePath(r.URL.Query().Get("path"))
	if folder == "" {
		httpError(w, http.StatusBadRequest, "which folder?")
		return
	}
	all, err := s.store.ListSongbookFiles(r.Context(), []int64{sb.ID})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	files := filesUnder(all, folder)
	for _, f := range files {
		if err := s.store.DeleteSongbookFile(r.Context(), f.ID); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": len(files)})
}
