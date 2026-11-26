package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// A band's recordings heard outside the band's own pages — a public
// song's recordings, a songbook's playlist, a set list's player — stream
// through the app (/recordings/{id}/audio): the app checks the listener
// may hear the recording, asks pCloud for the file with the band's folder
// link, and passes the bytes on, Range requests included. The folder link
// stays on the server: whoever has it lists the band's whole folder.
// (The band's own pages, its members only, still let the browser ask
// pCloud itself: docs/band-recordings.md.)

// RecordingFile is a recording someone may hear, as the server fetches it.
type RecordingFile struct {
	ID          int64
	BandID      int64
	FileID      int64
	Name        string
	Size        int64
	ContentType string
	apiHost     string
	code        string
}

// canHearSQL is "person $2 may hear the recording r" (s its song, LEFT
// JOINed): one of the band's current members, or a recording everyone
// signed in hears — a public song's, or the master of a public album's
// track (albums.go).
const canHearSQL = `(NOT r.gone AND (EXISTS (SELECT 1 FROM band_members bm WHERE bm.band_id = r.band_id AND bm.person_id = $2 AND upper_inf(bm.validity))
	OR coalesce(` + publicRecordingSQL + `, false)
	OR EXISTS (SELECT 1 FROM album_tracks at JOIN albums a ON a.id = at.album_id
	           WHERE at.recording_id = r.id AND a.public AND a.band_id = r.band_id)))`

// RecordingFileFor is recordingID for personID: ErrNotFound when there is
// no such recording (or it is gone from the folder), ErrForbidden when
// personID may not hear it.
func (st *Store) RecordingFileFor(ctx context.Context, personID, recordingID int64) (*RecordingFile, error) {
	var f RecordingFile
	var gone, ok bool
	err := st.db.QueryRow(ctx, `
		SELECT r.id, r.band_id, r.file_id, r.name, r.size, r.content_type, src.api_host, src.code, r.gone, `+canHearSQL+`
		FROM band_recordings r JOIN band_recording_sources src ON src.id = r.source_id
		LEFT JOIN songs s ON s.id = r.variant_id
		WHERE r.id = $1`, recordingID, personID).
		Scan(&f.ID, &f.BandID, &f.FileID, &f.Name, &f.Size, &f.ContentType, &f.apiHost, &f.code, &gone, &ok)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && gone) {
		return nil, fmt.Errorf("no such recording %d: %w", recordingID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	if !ok || personID <= 0 {
		return nil, fmt.Errorf("person %d may not hear recording %d: %w", personID, recordingID, ErrForbidden)
	}
	return &f, nil
}

// CanHearRecording reports whether personID may hear recordingID.
func (st *Store) CanHearRecording(ctx context.Context, personID, recordingID int64) bool {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT `+canHearSQL+` FROM band_recordings r LEFT JOIN songs s ON s.id = r.variant_id WHERE r.id = $1`,
		recordingID, personID).Scan(&ok)
	return err == nil && ok && personID > 0
}

// pcloudClient is the server's client for pCloud: short timeouts to
// connect and for the answer's headers; a file's body streams for as long
// as the request's context lasts.
var pcloudClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("pCloud: too many redirects")
		}
		return nil
	},
}

// pcloudDo sends a request to pCloud; tests replace it.
var pcloudDo = func(req *http.Request) (*http.Response, error) { return pcloudClient.Do(req) }

// pcloudLinks caches the file links pCloud gives the server (good for
// the server's own address, for a few hours): one ask a file an hour.
var pcloudLinks = struct {
	sync.Mutex
	m map[string]pcloudCached
}{m: map[string]pcloudCached{}}

type pcloudCached struct {
	url   string
	until time.Time
}

const pcloudLinkTTL = time.Hour

// pcloudFileLink is where the server downloads f from, asked of pCloud
// with the folder's link (getpublinkdownload), cached; fresh asks again.
func pcloudFileLink(ctx context.Context, f *RecordingFile, fresh bool) (string, error) {
	key := f.apiHost + "/" + f.code + "/" + fmt.Sprint(f.FileID)
	pcloudLinks.Lock()
	c, ok := pcloudLinks.m[key]
	pcloudLinks.Unlock()
	if ok && !fresh && time.Now().Before(c.until) {
		return c.url, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://"+f.apiHost+"/getpublinkdownload?code="+url.QueryEscape(f.code)+"&fileid="+fmt.Sprint(f.FileID), nil)
	if err != nil {
		return "", err
	}
	res, err := pcloudDo(req)
	if err != nil {
		return "", fmt.Errorf("pCloud: %w", err)
	}
	defer res.Body.Close()
	var out struct {
		Result int      `json:"result"`
		Error  string   `json:"error"`
		Hosts  []string `json:"hosts"`
		Path   string   `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&out); err != nil {
		return "", fmt.Errorf("pCloud: %w", err)
	}
	if out.Result != 0 || len(out.Hosts) == 0 {
		return "", fmt.Errorf("pCloud: %s (%d)", out.Error, out.Result)
	}
	link := "https://" + out.Hosts[0] + out.Path
	pcloudLinks.Lock()
	pcloudLinks.m[key] = pcloudCached{url: link, until: time.Now().Add(pcloudLinkTTL)}
	pcloudLinks.Unlock()
	return link, nil
}

// OpenRecording asks pCloud for f's bytes, rangeHeader passed on (""
// for the whole file). The caller closes the answer's body. A link pCloud
// no longer takes is asked for again, once.
func (st *Store) OpenRecording(ctx context.Context, f *RecordingFile, rangeHeader string) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		link, err := pcloudFileLink(ctx, f, attempt > 0)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
		if err != nil {
			return nil, err
		}
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		res, err := pcloudDo(req)
		if err != nil {
			return nil, fmt.Errorf("pCloud: %w", err)
		}
		switch res.StatusCode {
		case http.StatusForbidden, http.StatusGone, http.StatusNotFound, http.StatusGatewayTimeout:
			res.Body.Close()
			if attempt == 0 {
				continue
			}
			return nil, fmt.Errorf("pCloud: %s", res.Status)
		}
		return res, nil
	}
	return nil, errors.New("pCloud: no link")
}
