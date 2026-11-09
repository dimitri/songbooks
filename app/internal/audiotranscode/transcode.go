// Package audiotranscode shells out to ffmpeg (already a deployed
// dependency — see deploy/Dockerfile, installed for yt-dlp's own
// postprocessing) to compress an uploaded reference-audio file before
// it's stored. Confirmed a real need this session, not a hypothetical
// one: a real Guitar Pro file's embedded backing track (Mottainai
// Structure SD.gp) turned out to be a 67MB uncompressed WAV — well past
// handlePutSongAudio's 50MB storage ceiling, and needlessly bloating
// Postgres bytea storage (app/docs/mp3-reference-audio.md) for audio an
// MP3 represents in a tenth the size with no audible loss for a
// reference/practice track.
package audiotranscode

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

const Timeout = 3 * time.Minute

// looksCompressed reports whether data already looks like a compressed
// format (MP3 — an ID3 tag or a raw MPEG frame sync — or M4A/AAC/MP4's
// ftyp box) that ffmpeg doesn't need to touch. WAV ("RIFF....WAVE") and
// AIFF ("FORM....AIFF") are the two uncompressed container formats a GP
// file or a direct recording upload realistically shows up as; anything
// unrecognized is left alone by NeedsTranscode rather than guessed at.
func looksCompressed(data []byte) bool {
	if len(data) < 12 {
		return true // too short to be worth shelling out for; let it through as-is
	}
	if bytes.HasPrefix(data, []byte("ID3")) {
		return true
	}
	if data[0] == 0xFF && data[1]&0xE0 == 0xE0 { // MPEG frame sync
		return true
	}
	if bytes.Equal(data[4:8], []byte("ftyp")) { // M4A/AAC/MP4 container
		return true
	}
	return false
}

// NeedsTranscode reports whether data is an uncompressed container
// (WAV/AIFF) that ToMP3 should be run on before storing.
func NeedsTranscode(data []byte) bool {
	if looksCompressed(data) {
		return false
	}
	if len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WAVE")) {
		return true
	}
	if len(data) >= 12 && bytes.Equal(data[0:4], []byte("FORM")) && bytes.Equal(data[8:12], []byte("AIFF")) {
		return true
	}
	return false
}

// ToMP3 transcodes data (WAV/AIFF or anything else ffmpeg can decode)
// to a 128kbps stereo MP3, via ffmpeg's own stdin/stdout pipes — no
// temp files, so concurrent calls never collide and nothing lingers on
// disk if the request is cancelled.
func ToMP3(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", "pipe:0",
		"-codec:a", "libmp3lame", "-b:a", "128k", "-ac", "2",
		"-f", "mp3", "pipe:1")
	cmd.Stdin = bytes.NewReader(data)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg transcode: %w: %s", err, trimOutput(stderr.Bytes()))
	}
	return out.Bytes(), nil
}

func trimOutput(b []byte) string {
	const max = 500
	if len(b) > max {
		return string(b[len(b)-max:])
	}
	return string(b)
}
