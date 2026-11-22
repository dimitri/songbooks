package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"
	"time"

	"app/internal/store"
)

// A band's recording, heard through the app (store/band_recordings_stream.go):
// a public song's recordings, a songbook's playlist, a set list's player.
// The band's pCloud folder link stays on the server.

// maxRecordingBytes is the largest recording the app passes on: a
// concert's video, not a whole day's raw.
const maxRecordingBytes = 2 << 30

// recordingStreamTimeout bounds one answer: long enough for a slow
// listener to take a whole concert video, never forever.
const recordingStreamTimeout = 30 * time.Minute

// recordingAudioURL is where a page plays recording id from.
func recordingAudioURL(id int64) string { return fmt.Sprintf("/recordings/%d/audio", id) }

// handleRecordingAudio is GET /recordings/{id}/audio: the recording's
// bytes for who may hear it (one of the band's current members, or anyone
// signed in for a public one: store.RecordingFileFor), Range requests
// passed on to pCloud for seeking, cached by the browser (privately: it's
// checked per person).
func (s *Server) handleRecordingAudio(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := s.store.RecordingFileFor(r.Context(), personFromContext(r).ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.NotFound(w, r)
		return
	case errors.Is(err, store.ErrForbidden):
		http.Error(w, "you can't hear this recording", http.StatusForbidden)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if f.Size > maxRecordingBytes {
		http.Error(w, "this recording is too large to play here", http.StatusRequestEntityTooLarge)
		return
	}
	etag := fmt.Sprintf(`"rec-%d-%d-%d"`, f.ID, f.FileID, f.Size)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Accept-Ranges", "bytes")
	if r.Header.Get("Range") == "" && r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), recordingStreamTimeout)
	defer cancel()
	res, err := s.store.OpenRecording(ctx, f, r.Header.Get("Range"))
	if err != nil {
		log.Printf("recording %d: %v", f.ID, err)
		http.Error(w, "pCloud didn't give the recording", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK, http.StatusPartialContent, http.StatusRequestedRangeNotSatisfiable:
	default:
		log.Printf("recording %d: pCloud answered %s", f.ID, res.Status)
		http.Error(w, "pCloud didn't give the recording", http.StatusBadGateway)
		return
	}
	if res.ContentLength > maxRecordingBytes {
		http.Error(w, "this recording is too large to play here", http.StatusRequestEntityTooLarge)
		return
	}
	ct := f.ContentType
	if ct == "" || ct == "application/octet-stream" {
		ct = mime.TypeByExtension(path.Ext(f.Name))
	}
	if ct == "" {
		ct = res.Header.Get("Content-Type")
	}
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	for _, h := range []string{"Content-Range", "Last-Modified"} {
		if v := res.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	if res.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(res.ContentLength, 10))
	}
	w.WriteHeader(res.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, io.LimitReader(res.Body, maxRecordingBytes))
}
