// Package ytaudio shells out to yt-dlp to extract a YouTube video's audio
// as MP3 bytes — see app/docs/mp3-reference-audio.md §1 for why this is a
// self-hosted binary, not a third-party download API, and the legal
// reasoning accepted for this invite-only deployment. Shared by the
// app's background fetch (httpapi/yt_audio_jobs.go), band proposals and
// the admin tools so there's exactly one place that knows how to invoke
// yt-dlp.
package ytaudio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Binary is the yt-dlp program run: SONGBOOKS_YTDLP (cmd/songbooks) can
// point it at another one (a local stand-in that prints progress, for a
// stack without yt-dlp).
var Binary = "yt-dlp"

const Timeout = 5 * time.Minute

// JobTimeout bounds one background fetch (a long video on a slow line).
const JobTimeout = 15 * time.Minute

// MaxDuration is the longest video the background fetch keeps: a song,
// a lesson, not an album (about 50 MB of MP3 at yt-dlp's best quality).
const MaxDuration = 30 * time.Minute

// Download runs yt-dlp against url in a fresh temp directory (always
// removed afterward, so concurrent calls never collide on a filename and
// nothing lingers on disk if the caller is interrupted), extracting and
// transcoding to MP3, and returns the resulting bytes.
func Download(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	res, err := fetch(ctx, url, 0, nil)
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

// Progress is how far a fetch is: Fraction of the whole (the download
// is most of it, the conversion to MP3 the rest), ETA in seconds (-1:
// unknown), Phase "download" or "convert".
type Progress struct {
	Fraction float64
	ETA      int
	Phase    string
}

// Result is a fetch's audio and the video's length in seconds (0:
// unknown).
type Result struct {
	Data     []byte
	Duration float64
}

// Kind is why a fetch failed, as the song page says it.
type Kind string

const (
	KindAgeRestricted Kind = "age_restricted" // YouTube wants a signed-in adult
	KindUnavailable   Kind = "unavailable"    // removed, private, members only, not a video
	KindBlocked       Kind = "blocked"        // refused to this server: rate-limited, a bot check, a country
	KindTooLong       Kind = "too_long"       // longer than MaxDuration
	KindOther         Kind = "other"
)

// Error is a failed fetch: its Kind, whether trying again later may work
// (Transient), and yt-dlp's last words (Detail).
type Error struct {
	Kind      Kind
	Transient bool
	Detail    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("yt-dlp: %s: %s", e.Kind, e.Detail)
}

// Fetch is the background fetch: Download with progress reports
// (onProgress, from yt-dlp's --newline lines; it may be nil), the
// video's length, MaxDuration enforced, JobTimeout, and a failure told
// as an *Error.
func Fetch(ctx context.Context, url string, onProgress func(Progress)) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, JobTimeout)
	defer cancel()
	return fetch(ctx, url, MaxDuration, onProgress)
}

func fetch(ctx context.Context, url string, maxDur time.Duration, onProgress func(Progress)) (*Result, error) {
	dir, err := os.MkdirTemp("", "ytdlp-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	out := filepath.Join(dir, "audio.%(ext)s")
	durFile := filepath.Join(dir, "duration.txt")
	args := []string{"-x", "--audio-format", "mp3", "--audio-quality", "0",
		"--no-playlist", "--newline", "--progress", "--print-to-file", "%(duration)s", durFile}
	if maxDur > 0 {
		args = append(args, "--match-filter", fmt.Sprintf("!duration | duration <= %d", int(maxDur.Seconds())))
	}
	args = append(args, "-o", out, "--", url)
	cmd := exec.CommandContext(ctx, Binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr tail
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, &Error{Kind: KindOther, Transient: true, Detail: err.Error()}
	}
	var lines tail
	tracker := progressTracker{}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		lines.Write([]byte(line + "\n"))
		if p, ok := tracker.feed(line); ok && onProgress != nil {
			onProgress(p)
		}
	}
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	said := lines.String() + stderr.String()
	if ctx.Err() != nil {
		return nil, &Error{Kind: KindOther, Transient: true, Detail: "timed out: " + trimOutput([]byte(said))}
	}
	if waitErr != nil {
		return nil, Classify(said)
	}
	data, err := os.ReadFile(filepath.Join(dir, "audio.mp3"))
	if err != nil {
		// Exit 0 and no file: the length filter skipped it (it says so,
		// unless quiet).
		if e := Classify(said); e.Kind == KindTooLong || maxDur > 0 {
			return nil, &Error{Kind: KindTooLong, Detail: e.Detail}
		}
		return nil, &Error{Kind: KindOther, Transient: true, Detail: "yt-dlp reported success but no audio.mp3 was produced: " + trimOutput([]byte(said))}
	}
	res := &Result{Data: data}
	if b, err := os.ReadFile(durFile); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if d, err := strconv.ParseFloat(f[0], 64); err == nil && d > 0 {
				res.Duration = d
			}
		}
	}
	if maxDur > 0 && res.Duration > maxDur.Seconds() {
		return nil, &Error{Kind: KindTooLong, Detail: fmt.Sprintf("%.0f s", res.Duration)}
	}
	return res, nil
}

// progressLine is yt-dlp's --newline progress, e.g.
//
//	[download]  42.0% of    3.45MiB at  1.23MiB/s ETA 00:02
//	[download]  42.0% of ~  3.45MiB at  1.23MiB/s ETA 00:02 (frag 3/10)
//	[download] 100% of    3.45MiB in 00:00:02 at 1.50MiB/s
var progressLine = regexp.MustCompile(`^\[download\]\s+(\d+(?:\.\d+)?)%(?:.*?\bETA\s+(\S+))?`)

// ParseProgress reads one line of yt-dlp's output: the download's share
// done (0..100) and its ETA in seconds (-1 unknown); ok false for a line
// that isn't progress.
func ParseProgress(line string) (percent float64, eta int, ok bool) {
	m := progressLine.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return 0, -1, false
	}
	p, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, -1, false
	}
	return p, parseETA(m[2]), true
}

// parseETA reads "05", "01:02", "1:02:03"; -1 for "Unknown", "--:--".
func parseETA(s string) int {
	if s == "" {
		return -1
	}
	total := 0
	for _, part := range strings.Split(s, ":") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return -1
		}
		total = total*60 + n
	}
	return total
}

// downloadShare: the download's part of the whole, the conversion to MP3
// the rest.
const downloadShare = 0.9

// progressTracker turns yt-dlp's lines into Progress, never going
// backwards (a second stream starts again at 0%).
type progressTracker struct {
	last Progress
	seen bool
}

func (t *progressTracker) feed(line string) (Progress, bool) {
	var p Progress
	switch {
	case strings.HasPrefix(line, "[ExtractAudio]"):
		p = Progress{Fraction: downloadShare + 0.02, ETA: -1, Phase: "convert"}
	default:
		pct, eta, ok := ParseProgress(line)
		if !ok {
			return Progress{}, false
		}
		p = Progress{Fraction: downloadShare * pct / 100, ETA: eta, Phase: "download"}
	}
	if t.seen && p.Fraction < t.last.Fraction {
		return Progress{}, false
	}
	t.last, t.seen = p, true
	return p, true
}

// Classify reads why yt-dlp failed from what it said.
func Classify(output string) *Error {
	low := strings.ToLower(output)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(low, s) {
				return true
			}
		}
		return false
	}
	detail := lastError(output)
	switch {
	case has("does not pass filter"):
		return &Error{Kind: KindTooLong, Detail: detail}
	case has("confirm your age", "age-restricted", "age restricted", "inappropriate for some users"):
		return &Error{Kind: KindAgeRestricted, Detail: detail}
	case has("not available in your country", "blocked it in your country", "geo restrict", "geo-restrict", "not made this video available in your country"):
		return &Error{Kind: KindBlocked, Detail: detail}
	case has("http error 429", "too many requests", "not a bot", "rate-limit", "rate limit", "http error 403"):
		return &Error{Kind: KindBlocked, Transient: true, Detail: detail}
	case has("private video", "video unavailable", "has been removed", "no longer available", "account associated with this video has been terminated",
		"does not exist", "unsupported url", "is not a valid url", "members-only", "join this channel", "premieres in", "live event will begin", "incomplete youtube id"):
		return &Error{Kind: KindUnavailable, Detail: detail}
	}
	return &Error{Kind: KindOther, Transient: true, Detail: detail}
}

// lastError is yt-dlp's last "ERROR:" line, else the end of what it
// said.
func lastError(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "ERROR:") {
			return strings.TrimSpace(lines[i])
		}
	}
	return trimOutput([]byte(strings.TrimSpace(output)))
}

// AsError is err as an *Error (a plain error is KindOther, transient).
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Kind: KindOther, Transient: true, Detail: err.Error()}
}

func trimOutput(b []byte) string {
	const max = 500
	if len(b) > max {
		return string(b[len(b)-max:])
	}
	return string(b)
}

// tail keeps the last few KB written to it.
type tail struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if t.buf.Len() > 16<<10 {
		b := t.buf.Bytes()
		keep := append([]byte(nil), b[len(b)-8<<10:]...)
		t.buf.Reset()
		t.buf.Write(keep)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}
