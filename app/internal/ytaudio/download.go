// Package ytaudio shells out to yt-dlp to extract a YouTube video's audio
// as MP3 bytes — see app/docs/mp3-reference-audio.md §1 for why this is a
// self-hosted binary, not a third-party download API, and the legal
// reasoning accepted for this invite-only deployment. Shared by the
// httpapi upload handler and the cmd/backfillaudio one-off script so
// there's exactly one place that knows how to invoke yt-dlp.
package ytaudio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const Timeout = 5 * time.Minute

// Download runs yt-dlp against url in a fresh temp directory (always
// removed afterward, so concurrent calls never collide on a filename and
// nothing lingers on disk if the caller is interrupted), extracting and
// transcoding to MP3, and returns the resulting bytes.
func Download(ctx context.Context, url string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "ytdlp-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	out := filepath.Join(dir, "audio.%(ext)s")
	cmd := exec.CommandContext(ctx, "yt-dlp",
		"-x", "--audio-format", "mp3", "--audio-quality", "0",
		"--no-playlist", "-o", out, url)
	if combined, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, trimOutput(combined))
	}
	data, err := os.ReadFile(filepath.Join(dir, "audio.mp3"))
	if err != nil {
		return nil, fmt.Errorf("yt-dlp reported success but no audio.mp3 was produced: %w", err)
	}
	return data, nil
}

func trimOutput(b []byte) string {
	const max = 500
	if len(b) > max {
		return string(b[len(b)-max:])
	}
	return string(b)
}
