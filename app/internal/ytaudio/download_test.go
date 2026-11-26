package ytaudio

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// yt-dlp's --newline progress lines, as it prints them.
func TestParseProgress(t *testing.T) {
	cases := []struct {
		line string
		pct  float64
		eta  int
		ok   bool
	}{
		{"[download]   0.0% of    3.45MiB at  Unknown B/s ETA Unknown", 0, -1, true},
		{"[download]  42.0% of    3.45MiB at  1.23MiB/s ETA 00:02", 42, 2, true},
		{"[download]  12.5% of ~  9.10MiB at  512.00KiB/s ETA 01:05 (frag 3/24)", 12.5, 65, true},
		{"[download]  57.3% of   80.00MiB at    2.00MiB/s ETA 1:02:03", 57.3, 3723, true},
		{"[download] 100% of    3.45MiB in 00:00:02 at 1.50MiB/s", 100, -1, true},
		{"[download] 100.0% of    3.45MiB at    1.50MiB/s ETA 00:00", 100, 0, true},
		{"[download]  33.3% of    3.45MiB at  1.23MiB/s ETA --:--", 33.3, -1, true},
		{"[download] Destination: /tmp/ytdlp-1/audio.webm", 0, -1, false},
		{"[youtube] Extracting URL: https://www.youtube.com/watch?v=AAAAAAAAAAA", 0, -1, false},
		{"[ExtractAudio] Destination: /tmp/ytdlp-1/audio.mp3", 0, -1, false},
	}
	for _, c := range cases {
		pct, eta, ok := ParseProgress(c.line)
		if ok != c.ok || (ok && (pct != c.pct || eta != c.eta)) {
			t.Errorf("%q: %v %v %v, want %v %v %v", c.line, pct, eta, ok, c.pct, c.eta, c.ok)
		}
	}
}

// The whole's progress: the download 90 %, the conversion after it,
// never backwards (a second stream starting again at 0 %).
func TestProgressTracker(t *testing.T) {
	var tr progressTracker
	feed := func(line string) (Progress, bool) { return tr.feed(line) }
	if _, ok := feed("[youtube] AAAAAAAAAAA: Downloading webpage"); ok {
		t.Error("a line that isn't progress")
	}
	p, ok := feed("[download]  50.0% of 3.00MiB at 1.00MiB/s ETA 00:02")
	if !ok || p.Fraction != 0.45 || p.ETA != 2 || p.Phase != "download" {
		t.Errorf("half the download: %+v %v", p, ok)
	}
	if _, ok := feed("[download]   1.0% of 1.00MiB at 1.00MiB/s ETA 00:01"); ok {
		t.Error("went backwards")
	}
	p, ok = feed("[ExtractAudio] Destination: audio.mp3")
	if !ok || p.Phase != "convert" || p.Fraction <= 0.9 || p.Fraction >= 1 {
		t.Errorf("converting: %+v %v", p, ok)
	}
}

// What yt-dlp says when it fails, read as why.
func TestClassify(t *testing.T) {
	cases := []struct {
		out       string
		kind      Kind
		transient bool
	}{
		{"ERROR: [youtube] AAAAAAAAAAA: Sign in to confirm your age. This video may be inappropriate for some users.", KindAgeRestricted, false},
		{"ERROR: [youtube] AAAAAAAAAAA: Private video. Sign in if you've been granted access to this video", KindUnavailable, false},
		{"ERROR: [youtube] AAAAAAAAAAA: Video unavailable. This video has been removed by the uploader", KindUnavailable, false},
		{"ERROR: [youtube] AAAAAAAAAAA: Video unavailable. This video is no longer available because the YouTube account associated with this video has been terminated.", KindUnavailable, false},
		{"ERROR: [youtube] AAAAAAAAAAA: Join this channel to get access to members-only content like this video, and other exclusive perks.", KindUnavailable, false},
		{"ERROR: [youtube] AAAAAAAAAAA: Sign in to confirm you’re not a bot. Use --cookies-from-browser or --cookies for the authentication.", KindBlocked, true},
		{"ERROR: unable to download video data: HTTP Error 429: Too Many Requests", KindBlocked, true},
		{"ERROR: unable to download video data: HTTP Error 403: Forbidden", KindBlocked, true},
		{"ERROR: [youtube] AAAAAAAAAAA: Video unavailable. The uploader has not made this video available in your country", KindBlocked, false},
		{"[download] Some Song does not pass filter (!duration | duration <= 1800), skipping ..", KindTooLong, false},
		{"ERROR: [Errno -3] Temporary failure in name resolution", KindOther, true},
		{"", KindOther, true},
	}
	for _, c := range cases {
		e := Classify("[youtube] Extracting URL: https://www.youtube.com/watch?v=AAAAAAAAAAA\n" + c.out)
		if e.Kind != c.kind || e.Transient != c.transient {
			t.Errorf("%q: %s transient=%v, want %s transient=%v", c.out, e.Kind, e.Transient, c.kind, c.transient)
		}
	}
	if e := Classify("ERROR: one\nsomething\nERROR: the last one\nmore"); e.Detail != "ERROR: the last one" {
		t.Errorf("detail: %q", e.Detail)
	}
}

// fakeYTDLP writes a yt-dlp of our own: it prints progress and writes
// the MP3 and the video's length where it's told, or fails as told.
func fakeYTDLP(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script")
	}
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := Binary
	Binary = bin
	t.Cleanup(func() { Binary = old })
}

// The args parsed by the fake: the output template's directory and the
// length's file.
const fakeArgs = `
out=""; durfile=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
    --print-to-file) durfile="$3"; shift; shift ;;
  esac
  shift
done
dir=$(dirname "$out")
`

func TestFetch(t *testing.T) {
	fakeYTDLP(t, fakeArgs+`
echo "[youtube] Extracting URL: x"
echo "[download]  10.0% of 1.00MiB at 1.00MiB/s ETA 00:09"
echo "[download]  60.0% of 1.00MiB at 1.00MiB/s ETA 00:04"
echo "[download] 100% of 1.00MiB in 00:00:01 at 1.00MiB/s"
echo "[ExtractAudio] Destination: $dir/audio.mp3"
printf 'ID3fake' > "$dir/audio.mp3"
echo 222.5 > "$durfile"
`)
	var seen []Progress
	res, err := Fetch(context.Background(), "https://www.youtube.com/watch?v=AAAAAAAAAAA", func(p Progress) { seen = append(seen, p) })
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Data) != "ID3fake" || res.Duration != 222.5 {
		t.Errorf("result: %q %v", res.Data, res.Duration)
	}
	if len(seen) != 4 || seen[0].Fraction != 0.09 || seen[0].ETA != 9 || seen[3].Phase != "convert" {
		t.Errorf("progress: %+v", seen)
	}
}

func TestFetchFails(t *testing.T) {
	fakeYTDLP(t, fakeArgs+`
echo "[youtube] Extracting URL: x"
echo "ERROR: [youtube] AAAAAAAAAAA: Private video. Sign in if you've been granted access to this video" >&2
exit 1
`)
	_, err := Fetch(context.Background(), "https://www.youtube.com/watch?v=AAAAAAAAAAA", nil)
	e := AsError(err)
	if e.Kind != KindUnavailable || e.Transient || !strings.Contains(e.Detail, "Private video") {
		t.Errorf("%+v", e)
	}
	// Exit 0 and no file: the length filter skipped it.
	fakeYTDLP(t, fakeArgs+`echo "[download] Long one does not pass filter (!duration | duration <= 1800), skipping .."`)
	if _, err := Fetch(context.Background(), "https://youtu.be/AAAAAAAAAAA", nil); AsError(err).Kind != KindTooLong {
		t.Errorf("too long: %v", err)
	}
}
