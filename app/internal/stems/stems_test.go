package stems

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDemucs stands in for Demucs: it writes the four parts as Demucs
// does (out/MODEL/TRACK/PART.wav), each a copy of the input.
const fakeDemucs = `#!/bin/sh
while [ $# -gt 1 ]; do
  case "$1" in
    -n) model=$2; shift 2;;
    -o) out=$2; shift 2;;
    *) shift;;
  esac
done
in=$1
dir="$out/$model/$(basename "$in" .wav)"
mkdir -p "$dir"
for p in vocals drums bass other; do cp "$in" "$dir/$p.wav"; done
`

func TestSplit(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	demucs := filepath.Join(dir, "demucs")
	if err := os.WriteFile(demucs, []byte(fakeDemucs), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "a.mp3")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", src).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	audio, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Splitter{Demucs: []string{demucs}, Model: "htdemucs", Bitrate: 128}.Split(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range append(Parts, "no_vocals") {
		if len(got[p]) < 1000 {
			t.Errorf("%s: %d bytes", p, len(got[p]))
		}
	}
}

// SplitIn leaves the WAVs the analysis reads; Threads holds PyTorch to
// that many threads (OMP_NUM_THREADS), Jobs is Demucs' --jobs.
func TestSplitInThreads(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	seen := filepath.Join(dir, "seen")
	demucs := filepath.Join(dir, "demucs")
	script := "#!/bin/sh\necho \"$OMP_NUM_THREADS $MKL_NUM_THREADS $*\" > " + seen + "\n" + fakeDemucs[len("#!/bin/sh\n"):]
	if err := os.WriteFile(demucs, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "a.mp3")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", src).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	audio, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	parts, w, err := Splitter{Demucs: []string{demucs}, Model: "htdemucs", Threads: 4, Jobs: 3, Bitrate: 128}.SplitIn(context.Background(), work, audio)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts["no_vocals"]) < 1000 {
		t.Errorf("no_vocals: %d bytes", len(parts["no_vocals"]))
	}
	for _, f := range []string{w.Mix, w.Backing, w.Vocals} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("WAV gone: %v", err)
		}
	}
	got, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(got); !strings.HasPrefix(s, "4 4 ") || !strings.Contains(s, "--jobs 3") {
		t.Errorf("demucs ran with %q", s)
	}
}
