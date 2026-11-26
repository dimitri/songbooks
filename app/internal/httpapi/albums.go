package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math/rand/v2"
	"net/http"
	"path"
	"strconv"
	"strings"

	"app/internal/brand"
	"app/internal/i18n"
	"app/internal/pdfnative"
	"app/internal/song"
	"app/internal/store"
)

// Albums and live versions (docs/albums-and-live-versions.md;
// store/albums.go): a band's releases — their page, their editor, their
// artwork, their playlist and their PDF — the band's songbook by album,
// the song page's "From Ultrablanc (2026), track 3" and its Album | Live
// switch, and the live versions a band's set lists may use.

// albumOf is the album {albumID} of the band {id}, for the signed-in
// person, with their rights: a 404 when there's none or they may not see
// it.
func (s *Server) albumOf(w http.ResponseWriter, r *http.Request) (*store.Album, store.AlbumRights, bool) {
	bandID, err1 := pathID(r, "id")
	albumID, err2 := pathID(r, "albumID")
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return nil, store.AlbumRights{}, false
	}
	a, err := s.store.GetAlbum(r.Context(), albumID)
	if err != nil || a.BandID != bandID {
		http.NotFound(w, r)
		return nil, store.AlbumRights{}, false
	}
	rights, err := s.store.AlbumRightsFor(r.Context(), personFromContext(r).ID, a)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil, rights, false
	}
	if !rights.See {
		http.NotFound(w, r)
		return nil, rights, false
	}
	return a, rights, true
}

// writeAlbumErr answers a store error of an album's.
func writeAlbumErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrInvalid):
		httpError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrConflict):
		httpError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
	}
}

// albumKindLabel is an album's kind as the pages say it.
func albumKindLabel(kind string) string {
	switch kind {
	case "ep":
		return "EP"
	case "single":
		return "Single"
	case "live":
		return "Live album"
	}
	return "Album"
}

// albumTile is an album as the Albums page and the songbook show it.
type albumTile struct {
	*store.Album
	Href, KindLabel, Length string
}

func newAlbumTile(a *store.Album) albumTile {
	t := albumTile{Album: a, Href: fmt.Sprintf("/bands/%d/albums/%d", a.BandID, a.ID), KindLabel: albumKindLabel(a.Kind)}
	if n := a.Seconds(); n > 0 {
		t.Length = store.FormatDuration(n, true)
	}
	return t
}

// handleBandAlbumsPage is GET /bands/{id}/albums: the band's releases,
// newest first, the singles after the albums, as cover tiles; New album
// for whoever may edit the band's songbook.
func (s *Server) handleBandAlbumsPage(w http.ResponseWriter, r *http.Request) {
	bandID, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	band, err := s.store.GetBand(r.Context(), bandID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	albums, err := s.store.BandAlbums(r.Context(), bandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	canEdit, _ := s.bandPrivilege(r, bandID, store.BandPrivEditSongbook)
	var main, singles []albumTile
	for _, a := range albums {
		if a.Kind == "single" {
			singles = append(singles, newAlbumTile(a))
		} else {
			main = append(main, newAlbumTile(a))
		}
	}
	s.render(w, "band_albums", map[string]any{
		"Me": personFromContext(r), "Band": band, "Albums": main, "Singles": singles, "CanEdit": canEdit,
	})
}

// albumTrackRow is a track as the album page shows it.
type albumTrackRow struct {
	store.AlbumTrack
	Num       string
	Href      string // the song, when the viewer may open it
	LiveHref  string // its live version, when it has one the viewer sees
	SongTitle string // the song's own title, when the printed one differs
	Single    string // "also a single, 21 Nov 2025"
}

// handleAlbumPage is GET /bands/{id}/albums/{albumID}: the cover, the
// facts, Play the album, the links, the tracks; the editor for
// edit_songbook, Public for publish. The band's members see it; a public
// album, everyone signed in.
func (s *Server) handleAlbumPage(w http.ResponseWriter, r *http.Request) {
	a, rights, ok := s.albumOf(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	person := personFromContext(r)
	band, err := s.store.GetBand(ctx, a.BandID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	lang := s.langOf(w)
	member := person.IsAdmin
	if !member {
		member, _ = s.store.IsCurrentBandMember(ctx, a.BandID, person.ID)
	}
	all, err := s.store.BandAlbums(ctx, a.BandID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	singleOf := map[int64]*store.Album{}
	var more []albumTile
	for _, o := range all {
		if o.ID != a.ID && (member || o.Public) {
			more = append(more, newAlbumTile(o))
		}
		if o.Kind == "single" && o.ID != a.ID {
			for _, t := range o.Tracks {
				if t.SongID != nil {
					singleOf[*t.SongID] = o
				}
			}
		}
	}
	var rows []albumTrackRow
	hasLive, hasMaster := false, false
	for _, t := range a.Tracks {
		row := albumTrackRow{AlbumTrack: t, Num: strconv.Itoa(t.Position)}
		if t.Disc > 1 {
			row.Num = fmt.Sprintf("%d.%d", t.Disc, t.Position)
		}
		if t.SongID != nil && (member || t.SongPublic) {
			row.Href = fmt.Sprintf("/song/%d?band=%d", *t.SongID, a.BandID)
		}
		if t.SongID != nil && !strings.EqualFold(strings.TrimSpace(t.SongTitle), strings.TrimSpace(t.Title)) {
			row.SongTitle = t.SongTitle
		}
		if t.LiveID != 0 && member {
			row.LiveHref = fmt.Sprintf("/song/%d?band=%d", t.LiveID, a.BandID)
			hasLive = true
		}
		if t.SongID != nil && a.Kind != "single" {
			if o := singleOf[*t.SongID]; o != nil {
				when := strconv.Itoa(o.ReleaseYear)
				if o.ReleasedOn != nil {
					when = i18n.Date(lang, *o.ReleasedOn, "2 Jan 2006")
				}
				row.Single = i18n.T(lang, "also a single, %s", when)
			}
		}
		hasMaster = hasMaster || t.RecordingID != nil
		rows = append(rows, row)
	}
	data := map[string]any{
		"Me": person, "Band": band, "Album": a, "Tile": newAlbumTile(a), "Tracks": rows, "Rights": rights, "Member": member,
		"More": more, "HasLive": hasLive, "HasMaster": hasMaster,
		"Released": albumReleased(lang, a),
		"Play":     fmt.Sprintf("/bands/%d/albums/%d/play", a.BandID, a.ID),
		"PDF":      fmt.Sprintf("/bands/%d/albums/%d/export.pdf", a.BandID, a.ID),
	}
	if !member {
		// Not the band's: its page without the band's sections.
		data["Space"] = (*SpaceNav)(nil)
	}
	if rights.Edit {
		editor, err := s.albumEditorData(ctx, a)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data["Editor"] = editor
	}
	s.render(w, "band_album", data)
}

// albumReleased is "10 Apr 2026", "2026" or "".
func albumReleased(lang string, a *store.Album) string {
	if a.ReleasedOn != nil {
		return i18n.Date(lang, *a.ReleasedOn, "2 Jan 2006")
	}
	if a.ReleaseYear > 0 {
		return strconv.Itoa(a.ReleaseYear)
	}
	return ""
}

// albumEditorData is the editor's JSON: the album, its tracks, the
// band's songs a track may be (never a live version) and the masters of
// its folder.
func (s *Server) albumEditorData(ctx context.Context, a *store.Album) (template.JS, error) {
	entries, err := s.store.ListBandCatalog(ctx, a.BandID)
	if err != nil {
		return "", err
	}
	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.VariantID
	}
	lives, err := s.store.LiveOf(ctx, ids)
	if err != nil {
		return "", err
	}
	type songOpt struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	var songs []songOpt
	for _, e := range entries {
		if _, live := lives[e.VariantID]; live {
			continue
		}
		title := e.Title
		if e.VersionName != "" {
			title += " — " + e.VersionName
		}
		songs = append(songs, songOpt{e.VariantID, title})
	}
	masters, err := s.store.AlbumMasters(ctx, a.BandID)
	if err != nil {
		return "", err
	}
	type trackOut struct {
		Disc        int    `json:"disc"`
		SongID      int64  `json:"songId"`
		Title       string `json:"title"`
		DurationS   int    `json:"durationS"`
		Tempo       int    `json:"tempo"`
		ISRC        string `json:"isrc"`
		RecordingID int64  `json:"recordingId"`
		LinkURL     string `json:"linkUrl"`
	}
	tracks := []trackOut{}
	for _, t := range a.Tracks {
		o := trackOut{Disc: t.Disc, Title: t.Title, ISRC: t.ISRC, LinkURL: t.LinkURL}
		if t.SongID != nil {
			o.SongID = *t.SongID
		}
		if t.DurationS != nil {
			o.DurationS = *t.DurationS
		}
		if t.Tempo != nil {
			o.Tempo = *t.Tempo
		}
		if t.RecordingID != nil {
			o.RecordingID = *t.RecordingID
		}
		tracks = append(tracks, o)
	}
	released := ""
	if a.ReleasedOn != nil {
		released = a.ReleasedOn.Format("2006-01-02")
	}
	links := a.Links
	if links == nil {
		links = []store.AlbumLink{}
	}
	raw, err := json.Marshal(map[string]any{
		"id": a.ID, "bandId": a.BandID, "title": a.Title, "kind": a.Kind, "releasedOn": released, "releaseYear": a.ReleaseYear,
		"label": a.Label, "credits": a.Credits, "licence": a.Licence, "links": links, "tracks": tracks,
		"songs": songs, "masters": masters, "cover": a.CoverPath(),
	})
	return template.JS(raw), err
}

// handleCreateAlbum is POST /api/bands/{id}/albums, {"title", "kind",
// …AlbumInput}: the band's new album; answers its id and page.
func (s *Server) handleCreateAlbum(w http.ResponseWriter, r *http.Request) {
	bandID, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var in store.AlbumInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	id, err := s.store.CreateAlbum(r.Context(), personFromContext(r).ID, bandID, in)
	if err != nil {
		writeAlbumErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "href": fmt.Sprintf("/bands/%d/albums/%d", bandID, id)})
}

// handleSaveAlbum is PUT /api/bands/{id}/albums/{albumID}: its fields and
// tracks (store.AlbumInput), for edit_songbook.
func (s *Server) handleSaveAlbum(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.albumOf(w, r)
	if !ok {
		return
	}
	var in store.AlbumInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SaveAlbum(r.Context(), personFromContext(r).ID, a.ID, in); err != nil {
		writeAlbumErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true})
}

// handleDeleteAlbum is DELETE /api/bands/{id}/albums/{albumID}.
func (s *Server) handleDeleteAlbum(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.albumOf(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteAlbum(r.Context(), personFromContext(r).ID, a.ID); err != nil {
		writeAlbumErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "href": fmt.Sprintf("/bands/%d/albums", a.BandID)})
}

// handleSetAlbumPublic is POST /api/bands/{id}/albums/{albumID}/public,
// {"public": true, "songs": true}: for publish; with songs, its songs go
// public too (not their live versions).
func (s *Server) handleSetAlbumPublic(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.albumOf(w, r)
	if !ok {
		return
	}
	var req struct {
		Public bool `json:"public"`
		Songs  bool `json:"songs"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	n, err := s.store.SetAlbumPublic(r.Context(), personFromContext(r).ID, a.ID, req.Public, req.Songs)
	if err != nil {
		writeAlbumErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"public": req.Public, "songsMadePublic": n})
}

// handlePutAlbumCover is PUT /api/bands/{id}/albums/{albumID}/cover: an
// image uploaded as its artwork (multipart, field "file"), cropped square
// and resized by the server (brand.PrepareArtwork). Answers its address.
func (s *Server) handlePutAlbumCover(w http.ResponseWriter, r *http.Request) {
	a, rights, ok := s.albumOf(w, r)
	if !ok {
		return
	}
	if !rights.Edit {
		httpError(w, http.StatusForbidden, "your role in this band doesn't allow this")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, brand.MaxArtworkUpload+64<<10)
	if err := r.ParseMultipartForm(brand.MaxArtworkUpload + 64<<10); err != nil {
		httpError(w, http.StatusRequestEntityTooLarge, "the image is larger than 12 MB, or not sent as a file")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, brand.MaxArtworkUpload+1))
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	img, err := brand.PrepareArtwork(data)
	switch {
	case errors.Is(err, brand.ErrArtworkTooLarge):
		httpError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	case errors.Is(err, brand.ErrLogoFormat), errors.Is(err, brand.ErrLogoSVG):
		httpError(w, http.StatusUnsupportedMediaType, err.Error())
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.SetAlbumCover(r.Context(), a.ID, img, personFromContext(r).ID); err != nil {
		writeAlbumErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cover": store.AlbumCoverPath(a.ID, img.SHA256)})
}

// handleDeleteAlbumCover is DELETE /api/bands/{id}/albums/{albumID}/cover.
func (s *Server) handleDeleteAlbumCover(w http.ResponseWriter, r *http.Request) {
	a, rights, ok := s.albumOf(w, r)
	if !ok {
		return
	}
	if !rights.Edit {
		httpError(w, http.StatusForbidden, "your role in this band doesn't allow this")
		return
	}
	if err := s.store.DeleteAlbumCover(r.Context(), a.ID); err != nil {
		writeAlbumErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cover": ""})
}

// handleGetAlbumCover is GET /album-covers/{albumID}/{file}, file the
// artwork's hash and extension: no login (the songbook, the player's
// lock screen, a public album's page), its address unguessable and
// cached for good; a replaced artwork's address is not found.
func (s *Server) handleGetAlbumCover(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "albumID")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := s.store.GetAlbumCover(r.Context(), id)
	if err != nil || path.Base(store.AlbumCoverPath(id, c.SHA256)) != r.PathValue("file") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", c.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+c.SHA256+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", c.CreatedAt, bytes.NewReader(c.Data))
}

// handleAlbumPlaylist is GET /bands/{id}/albums/{albumID}/play: the album
// in the app's player (the playlist page), track by track, each track's
// master streamed from the band's folder (recordings_stream.go: for the
// band's members, or everyone signed in on a public album), else its
// song's own recording or reference; the artwork on the lock screen.
// ?shuffle=1 plays it in another order.
func (s *Server) handleAlbumPlaylist(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.albumOf(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	person := personFromContext(r)
	member := person.IsAdmin
	if !member {
		member, _ = s.store.IsCurrentBandMember(ctx, a.BandID, person.ID)
	}
	sbID, _ := s.store.GetDefaultSongbook(ctx, "band", a.BandID)
	var tracks, silent []playlistTrack
	for _, t := range a.Tracks {
		var tr playlistTrack
		seeSong := t.SongID != nil && (member || t.SongPublic)
		if seeSong {
			if pt, ok := s.playlistTrackFor(ctx, sbID, *t.SongID, ""); ok {
				tr = pt
			}
		}
		tr.Title = t.Title
		if tr.Href == "" {
			tr.Href = fmt.Sprintf("/bands/%d/albums/%d", a.BandID, a.ID)
		}
		if seeSong {
			tr.VariantID = *t.SongID
		}
		tr.Artist = a.BandName
		if t.RecordingID != nil && s.store.CanHearRecording(ctx, person.ID, *t.RecordingID) {
			tr.Source, tr.Category, tr.AudioURL, tr.VideoID, tr.SourceLabel = "band", "", recordingAudioURL(*t.RecordingID), "", "Album"
			for i := range tr.Sections {
				tr.Sections[i].Start = -1
			}
		}
		if tr.Sections == nil {
			tr.Sections = []playlistSection{}
		}
		tr.Num = strconv.Itoa(t.Position)
		tr.Subtitle = strings.Join(nonEmpty(a.BandName, t.Duration()), " · ")
		tr.Cover, tr.Album = a.CoverPath(), a.Title
		if tr.Source == "" {
			silent = append(silent, tr)
		} else {
			tracks = append(tracks, tr)
		}
	}
	if r.URL.Query().Get("shuffle") == "1" {
		rand.Shuffle(len(tracks), func(i, j int) { tracks[i], tracks[j] = tracks[j], tracks[i] })
	}
	tracksJSON, _ := json.Marshal(tracks)
	sb := &store.Songbook{ID: sbID, OwnerKind: "band", OwnerBandID: a.BandID}
	s.render(w, "playlist", map[string]any{
		"Me": person, "Songbook": sb, "Owner": a.Title, "Back": fmt.Sprintf("/bands/%d/albums/%d", a.BandID, a.ID),
		"Tracks": tracks, "Silent": silent, "TracksJSON": template.JS(tracksJSON),
		"StoreKey": fmt.Sprintf("songbooks:album:%d", a.ID),
	})
}

// handleExportAlbumPDF is GET /bands/{id}/albums/{albumID}/export.pdf: a
// cover page with the artwork, the track list, then the songs in track
// order (?live=1: their live versions instead, where they have one).
func (s *Server) handleExportAlbumPDF(w http.ResponseWriter, r *http.Request) {
	if s.pdf == nil {
		httpError(w, http.StatusServiceUnavailable, "PDF export is not configured on this server")
		return
	}
	a, _, ok := s.albumOf(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	person := personFromContext(r)
	live := r.URL.Query().Get("live") == "1"
	var refs []pdfnative.SongRef
	for _, t := range a.Tracks {
		if t.SongID == nil {
			continue
		}
		id := *t.SongID
		if live && t.LiveID != 0 {
			id = t.LiveID
		}
		if ok, err := s.store.CanViewVariant(ctx, person.ID, id); err != nil || !ok {
			continue
		}
		sg, _, err := s.store.LoadVariant(id)
		if err != nil {
			continue
		}
		refs = append(refs, pdfnative.SongRef{Stem: strconv.FormatInt(id, 10), Body: song.Write(sg)})
	}
	if len(refs) == 0 {
		httpError(w, http.StatusNotFound, "this album has no songs to print")
		return
	}
	front := pdfnative.FrontMatter{CoverTitle: a.Title, BandName: a.BandName, Date: albumReleased(s.langOf(w), a)}
	if c, err := s.store.GetAlbumCover(ctx, a.ID); err == nil {
		front.Image = c.Data
	}
	s.withJazzCharts(r, refs)
	data, err := s.pdf.ExportSetlist(ctx, a.Title, front, refs, optionsFromRequest(r))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "rendering PDF: "+err.Error())
		return
	}
	writePDF(w, a.BandName+"-"+a.Title, data)
}

// The band's songbook by album (band_catalog.html): each album a section,
// its songs in track order; the singles' songs that are on no album; then
// the songs on none. A live version is no row of its own: its album
// song's row has the Live chip.

// albumGroup is one section of the songbook by album.
type albumGroup struct {
	Tile    *albumTile // nil: Singles, Not on an album
	Heading string
	Key     string // its filter chip's key
	Entries []bandCatalogRow
}

// songbookByAlbum folds bandID's live versions into their album songs'
// rows (LiveID) and, when the band has albums, groups the rows by album.
// It answers the rows to list (no live version among them), the groups
// (nil without albums) and the albums.
func (s *Server) songbookByAlbum(ctx context.Context, bandID int64, rows []bandCatalogRow) ([]bandCatalogRow, []albumGroup, []*store.Album, error) {
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.VariantID
	}
	lives, err := s.store.LiveOf(ctx, ids)
	if err != nil {
		return nil, nil, nil, err
	}
	liveOf := map[int64]int64{} // album song -> its live version
	for l, song := range lives {
		liveOf[song] = l
	}
	flat := make([]bandCatalogRow, 0, len(rows))
	index := map[int64]int{}
	for _, row := range rows {
		if _, isLive := lives[row.VariantID]; isLive {
			continue
		}
		row.LiveID = liveOf[row.VariantID]
		index[row.VariantID] = len(flat)
		flat = append(flat, row)
	}
	// A live version whose album song isn't in the songbook keeps its row.
	for _, row := range rows {
		if song, isLive := lives[row.VariantID]; isLive {
			if _, there := index[song]; !there {
				flat = append(flat, row)
			}
		}
	}
	albums, err := s.store.BandAlbums(ctx, bandID)
	if err != nil || len(albums) == 0 {
		return flat, nil, albums, err
	}
	placed := map[int64]bool{}
	var groups []albumGroup
	for _, a := range albums {
		if a.Kind == "single" {
			continue
		}
		tile := newAlbumTile(a)
		g := albumGroup{Tile: &tile, Key: fmt.Sprintf("album-%d", a.ID)}
		for _, t := range a.Tracks {
			if t.SongID == nil {
				continue
			}
			i, ok := index[*t.SongID]
			if !ok {
				continue
			}
			row := flat[i]
			row.Track = t.Position
			if !strings.EqualFold(strings.TrimSpace(t.Title), strings.TrimSpace(row.Title)) {
				row.PrintedTitle = t.Title
			}
			g.Entries = append(g.Entries, row)
			placed[*t.SongID] = true
		}
		groups = append(groups, g)
	}
	singles := albumGroup{Heading: "Singles", Key: "singles"}
	for _, a := range albums {
		if a.Kind != "single" {
			continue
		}
		for _, t := range a.Tracks {
			if t.SongID == nil || placed[*t.SongID] {
				continue
			}
			if i, ok := index[*t.SongID]; ok {
				singles.Entries = append(singles.Entries, flat[i])
				placed[*t.SongID] = true
			}
		}
	}
	if len(singles.Entries) > 0 {
		groups = append(groups, singles)
	}
	rest := albumGroup{Heading: "Not on an album", Key: "none"}
	for _, row := range flat {
		if !placed[row.VariantID] {
			rest.Entries = append(rest.Entries, row)
		}
	}
	if len(rest.Entries) > 0 {
		groups = append(groups, rest)
	}
	return flat, groups, albums, nil
}

// The song page (play.html): where the song comes from, "From Ultrablanc
// (2026), track 3 · also a single, 21 Nov 2025", and the Album | Live
// switch.

// songAlbumLine is the song page's album line and switch.
type songAlbumLine struct {
	Album     *songAlbumRef
	Singles   []songAlbumRef
	SongID    int64
	AlbumSong int64 // the album song (this one, or the live version's)
	LiveID    int64 // its live version, 0: none the viewer sees
	IsLive    bool
	CanMake   bool // edit_songbook, an album song with no live version
	Query     string
}

type songAlbumRef struct {
	store.SongAlbum
	Href, When string
}

// albumLineFor is variantID's album line for the person, nil when the
// song is on no album they see and has no live version.
func (s *Server) albumLineFor(r *http.Request, lang string, variantID int64) *songAlbumLine {
	ctx := r.Context()
	person := personFromContext(r)
	if person == nil {
		return nil
	}
	albums, err := s.store.AlbumsOfSong(ctx, variantID)
	if err != nil {
		return nil
	}
	line := &songAlbumLine{SongID: variantID, AlbumSong: variantID}
	if q := r.URL.RawQuery; q != "" {
		line.Query = "?" + q
	}
	if of, err := s.store.LiveOf(ctx, []int64{variantID}); err == nil {
		if song, ok := of[variantID]; ok {
			line.IsLive, line.AlbumSong, line.LiveID = true, song, variantID
		}
	}
	if !line.IsLive {
		if l, err := s.store.LiveVersionsOf(ctx, []int64{variantID}); err == nil && l[variantID] != 0 {
			if ok, _ := s.store.CanViewVariant(ctx, person.ID, l[variantID]); ok {
				line.LiveID = l[variantID]
			}
		}
	}
	member := map[int64]bool{}
	for _, a := range albums {
		m, seen := member[a.BandID]
		if !seen {
			m = person.IsAdmin
			if !m {
				m, _ = s.store.IsCurrentBandMember(ctx, a.BandID, person.ID)
			}
			member[a.BandID] = m
		}
		if !m && !a.Public {
			continue
		}
		ref := songAlbumRef{SongAlbum: a, Href: fmt.Sprintf("/bands/%d/albums/%d", a.BandID, a.AlbumID)}
		if a.ReleasedOn != nil {
			ref.When = i18n.Date(lang, *a.ReleasedOn, "2 Jan 2006")
		} else if a.Year > 0 {
			ref.When = strconv.Itoa(a.Year)
		}
		if a.Kind == "single" {
			line.Singles = append(line.Singles, ref)
		} else if line.Album == nil {
			line.Album = &ref
		}
	}
	if line.Album == nil && len(line.Singles) > 0 {
		line.Album, line.Singles = &line.Singles[0], line.Singles[1:]
	}
	if !line.IsLive && line.LiveID == 0 && line.Album != nil {
		if band, err := s.store.SongOwnerBand(ctx, variantID); err == nil && band != 0 {
			line.CanMake, _ = s.bandPrivilege(r, band, store.BandPrivEditSongbook)
		}
	}
	if line.Album == nil && line.LiveID == 0 {
		return nil
	}
	return line
}

// handleMakeLiveVersion is POST /api/songs/{id}/live-version, {"moveSetlists":
// bool, "name": ""}: the live version of the band's song {id}, for
// edit_songbook (store.MakeLiveVersion). Set lists move to it when asked,
// or when the band's set lists use live versions. Answers its id.
func (s *Server) handleMakeLiveVersion(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var req struct {
		MoveSetlists *bool  `json:"moveSetlists"`
		Name         string `json:"name"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	ctx := r.Context()
	move := false
	if req.MoveSetlists != nil {
		move = *req.MoveSetlists
	} else if band, err := s.store.SongOwnerBand(ctx, id); err == nil && band != 0 {
		v, _ := s.store.BandSetlistVersions(ctx, band)
		move = v == "live"
	}
	liveID, err := s.store.MakeLiveVersion(ctx, personFromContext(r).ID, id, store.LiveVersionOptions{Name: req.Name, MoveSetlists: move})
	if err != nil {
		writeAlbumErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"variantId": liveID})
}

// handleSetSetlistVersions is POST /api/bands/{id}/setlist-versions,
// {"value": "live"|"song"}: what the band's set lists use (edit_band).
func (s *Server) handleSetSetlistVersions(w http.ResponseWriter, r *http.Request) {
	bandID, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetBandSetlistVersions(r.Context(), bandID, req.Value); err != nil {
		writeAlbumErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"value": req.Value})
}

// handleUseLiveVersions is POST /api/setlists/{id}/use-live-versions: a
// band's set list made before moves to the live versions (each line whose
// song has one), one change in its history; for whoever may edit it.
func (s *Server) handleUseLiveVersions(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	person := personFromContext(r)
	ok, err := s.store.CanEditSetlistByID(r.Context(), person.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		httpError(w, http.StatusForbidden, "no edit rights on this setlist")
		return
	}
	n, err := s.store.UseLiveVersions(r.Context(), person.ID, id)
	if err != nil {
		writeAlbumErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"moved": n})
}

// The band setting (Settings › The band): Set lists use the live version
// when there is one, or the song itself.

// setlistVersionsSetting is the setting as the settings page shows it,
// nil when the band has no albums and no live versions (nothing to
// choose).
type setlistVersionsSetting struct {
	BandID  int64
	Value   string
	CanEdit bool
	Lives   int
}

func (s *Server) setlistVersionsSettingFor(ctx context.Context, bandID int64, canEdit bool) *setlistVersionsSetting {
	hasLive, _ := s.store.HasLiveVersions(ctx, bandID)
	hasAlbums, _ := s.store.BandHasAlbums(ctx, bandID)
	if !hasLive && !hasAlbums {
		return nil
	}
	v, err := s.store.BandSetlistVersions(ctx, bandID)
	if err != nil {
		return nil
	}
	return &setlistVersionsSetting{BandID: bandID, Value: v, CanEdit: canEdit}
}

// The set list's page (setlist_overview.html): each line's Live chip, a
// line still on its album song marked "Album · Make the live version",
// "Use the live versions here", and the player's Live | Album.

// setlistLiveRow is a set list line's live/album state.
type setlistLiveRow struct {
	Live      bool  // it plays a live version
	AlbumSong int64 // the album song (for a live line)
	LiveID    int64 // an album song's live version (it could play)
	CanMake   bool  // an album song with none: Make the live version
}

// setlistLive is the set list page's live state.
type setlistLive struct {
	On      bool // the band's set lists use live versions: Live | Album
	ToMove  int  // lines on an album song that has a live version
	CanMake bool
}

// addSetlistLive marks sets' rows (a band's set list) with their live
// state and, when the band's set lists use live versions, makes the
// player's two choices Live (the line's version: its recording, else its
// reference) and Album (the album song: its master from the band's
// folder, else its own recording or reference). It reports whether some
// row has both (the player's choice).
func (s *Server) addSetlistLive(r *http.Request, bandID int64, sets []gigSet) (*setlistLive, bool) {
	ctx := r.Context()
	if bandID == 0 {
		return nil, false
	}
	var ids []int64
	for _, set := range sets {
		for _, row := range set.Rows {
			ids = append(ids, row.VariantID)
		}
	}
	lives, err := s.store.LiveOf(ctx, ids)
	if err != nil {
		return nil, false
	}
	liveOf, err := s.store.LiveVersionsOf(ctx, ids)
	if err != nil {
		return nil, false
	}
	hasLive, _ := s.store.HasLiveVersions(ctx, bandID)
	hasAlbums, _ := s.store.BandHasAlbums(ctx, bandID)
	if !hasLive && !hasAlbums {
		return nil, false
	}
	v, _ := s.store.BandSetlistVersions(ctx, bandID)
	st := &setlistLive{On: v == "live"}
	canMake, _ := s.bandPrivilege(r, bandID, store.BandPrivEditSongbook)
	st.CanMake = canMake
	var onAlbum map[int64]bool
	if canMake {
		onAlbum = map[int64]bool{}
		if albums, err := s.store.BandAlbums(ctx, bandID); err == nil {
			for song := range store.AlbumSongs(albums) {
				onAlbum[song] = true
			}
		}
	}
	person := personIDOf(r)
	choice := false
	for si := range sets {
		for ri := range sets[si].Rows {
			row := &sets[si].Rows[ri]
			lr := &setlistLiveRow{}
			if song, ok := lives[row.VariantID]; ok {
				lr.Live, lr.AlbumSong = true, song
				if row.VersionName == "Live" {
					row.VersionName = "" // the chip says it
				}
			} else if l := liveOf[row.VariantID]; l != 0 {
				lr.LiveID = l
				st.ToMove++
			} else if canMake && onAlbum[row.VariantID] {
				lr.CanMake = true
			}
			row.Live = lr
			if !st.On {
				continue
			}
			// Live | Album: the line's own sound, and its album song's.
			own := row.Sound
			albumSong := row.VariantID
			if lr.Live {
				albumSong = lr.AlbumSong
			}
			snd := &setlistSound{}
			if own != nil {
				snd.Rec = own.Rec
				if snd.Rec == nil {
					snd.Rec = own.Ref
				}
				if snd.Rec != nil {
					snd.Rec = &soundSource{Src: snd.Rec.Src, Label: "the live version"}
				}
			}
			snd.Ref = s.albumSoundFor(ctx, person, bandID, albumSong)
			if snd.Rec == nil && snd.Ref == nil {
				row.Sound = nil
				continue
			}
			row.Sound = snd
			// Live | Album shows whenever the list plays something: a line
			// with only one of them plays that one.
			choice = true
		}
	}
	return st, choice
}

// albumSoundFor is what the set list's Album plays for songID: its album
// master from the band's folder, else its own recording or reference.
func (s *Server) albumSoundFor(ctx context.Context, personID, bandID, songID int64) *soundSource {
	if albums, err := s.store.AlbumsOfSong(ctx, songID); err == nil {
		for _, a := range albums {
			al, err := s.store.GetAlbum(ctx, a.AlbumID)
			if err != nil {
				continue
			}
			for _, t := range al.Tracks {
				if t.SongID != nil && *t.SongID == songID && t.RecordingID != nil && s.store.CanHearRecording(ctx, personID, *t.RecordingID) {
					return &soundSource{Src: recordingAudioURL(*t.RecordingID), Label: "the album"}
				}
			}
		}
	}
	if snd := s.soundFor(ctx, personID, bandID, songID); snd != nil {
		src := snd.Rec
		if src == nil {
			src = snd.Ref
		}
		return &soundSource{Src: src.Src, Label: "the album"}
	}
	return nil
}

// liveSwapNewLines is, for a band's set list save, the new lines' songs
// (not in before) that play their live version instead, the band's set
// lists using live versions.
func (s *Server) liveSwapNewLines(ctx context.Context, bandID int64, had map[int64]bool, now []int64) map[int64]int64 {
	var fresh []int64
	for _, v := range now {
		if !had[v] {
			fresh = append(fresh, v)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	swap, err := s.store.LiveForNewLines(ctx, bandID, fresh)
	if err != nil || len(swap) == 0 {
		return nil
	}
	return swap
}

// foldLiveRepertoire drops the live versions from a band set list's
// list of songs to add: each album song is listed once (a new line picks
// its live version when the band's set lists use them).
func (s *Server) foldLiveRepertoire(ctx context.Context, repertoire []map[string]any) []map[string]any {
	var ids []int64
	for _, e := range repertoire {
		if id, ok := e["variantId"].(int64); ok {
			ids = append(ids, id)
		}
	}
	lives, err := s.store.LiveOf(ctx, ids)
	if err != nil || len(lives) == 0 {
		return repertoire
	}
	in := map[int64]bool{}
	for _, id := range ids {
		in[id] = true
	}
	liveOf := map[int64]bool{}
	for _, song := range lives {
		liveOf[song] = true
	}
	out := repertoire[:0]
	for _, e := range repertoire {
		id, _ := e["variantId"].(int64)
		if song, isLive := lives[id]; isLive && in[song] {
			continue
		}
		if liveOf[id] {
			e["live"] = true
		}
		out = append(out, e)
	}
	return out
}

// albumsChip puts the Albums view among a band's Songbook chips, after
// Songs, on a band with albums or original music.
func (s *Server) albumsChip(ctx context.Context, band *store.Band, section string, nav *SpaceNav) {
	if nav == nil || nav.Current != "catalog" || !nav.ViewsInPage {
		return
	}
	has, _ := s.store.BandHasAlbums(ctx, band.ID)
	if !has && band.MusicType != "original" {
		return
	}
	// Songs counts the album songs once: a live version is a chip of its
	// album song's row.
	if songs, err := s.store.ListBandCatalog(ctx, band.ID); err == nil {
		ids := make([]int64, len(songs))
		for i, e := range songs {
			ids[i] = e.VariantID
		}
		if lives, err := s.store.LiveOf(ctx, ids); err == nil && len(lives) > 0 {
			for i := range nav.Filters {
				if nav.Filters[i].Key == "all" {
					nav.Filters[i].Hint = fmt.Sprint(len(songs) - len(lives))
				}
			}
		}
	}
	on := section == "catalog/albums"
	chip := SpaceTab{SpaceSection: SpaceSection{Key: "albums", Label: "Albums", Path: "/albums"}, Href: nav.Home + "/albums", Current: on}
	var out []SpaceTab
	for _, f := range nav.Filters {
		if on {
			f.Current = false
		}
		out = append(out, f)
		if f.Key == "all" {
			out = append(out, chip)
		}
	}
	nav.Filters = out
}
