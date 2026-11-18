package stems

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
