// Package stems splits a recording into its parts with Demucs
// (https://github.com/facebookresearch/demucs): vocals, drums, bass and
// other, each an MP3, plus no_vocals (the three others mixed), what the
// practice player plays under a voice turned down. See
// docs/audio-cast-transpose-stems.md and cmd/stemsd.
package stems

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
)

// Splitter runs Demucs and ffmpeg.
type Splitter struct {
	// Demucs is the command, and its first arguments, that runs Demucs:
	// "demucs", or "python3 -m demucs".
	Demucs []string
	Model  string // "htdemucs"
	// Threads PyTorch may use in Demucs (OMP_NUM_THREADS), 0 for the
	// environment's.
	Threads int
	// Jobs: how many of the recording's segments Demucs separates at
	// once (its --jobs), each on Threads threads; 0 for one at a time.
	// On a many-core CPU this is what scales, not Threads alone
	// (docs/stems-and-key.md).
	Jobs int
	// Bitrate of the parts, in kbit/s.
	Bitrate int
}

// Parts are the four parts Demucs makes, in the order they're mixed.
var Parts = []string{"vocals", "drums", "bass", "other"}

// Split returns the parts of the recording in audio (any format ffmpeg
// reads), MP3 encoded, keyed by name.
func (s Splitter) Split(ctx context.Context, audio []byte) (map[string][]byte, error) {
	dir, err := os.MkdirTemp("", "stems-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	parts, _, err := s.SplitIn(ctx, dir, audio)
	return parts, err
}

// SplitIn splits like Split, working in dir, and leaves there the WAVs
// the analysis reads (no decoding the MP3s again): the caller removes
// dir.
func (s Splitter) SplitIn(ctx context.Context, dir string, audio []byte) (map[string][]byte, WAVs, error) {
	// A WAV first: Demucs reads whatever ffmpeg gives it the same way
	// every time, and a Guitar Pro recording's zero padding goes.
	in := filepath.Join(dir, "in.mp3")
	if err := os.WriteFile(in, audio, 0o600); err != nil {
		return nil, WAVs{}, err
	}
	wav := filepath.Join(dir, "track.wav")
	if err := run(ctx, "ffmpeg", "-nostdin", "-v", "error", "-i", in, "-ac", "2", "-ar", "44100", wav); err != nil {
		return nil, WAVs{}, fmt.Errorf("reading the recording: %w", err)
	}

	out := filepath.Join(dir, "out")
	args := append([]string{}, s.Demucs[1:]...)
	args = append(args, "-n", s.Model, "-d", "cpu", "-o", out)
	if s.Jobs > 0 {
		args = append(args, "--jobs", strconv.Itoa(s.Jobs))
	}
	args = append(args, wav)
	if err := runEnv(ctx, threadsEnv(s.Threads), s.Demucs[0], args...); err != nil {
		return nil, WAVs{}, fmt.Errorf("demucs: %w", err)
	}
	parts := filepath.Join(out, s.Model, "track")

	// The five MP3s at once: one after the other they took half a minute
	// on the production host, a single-threaded encoder each.
	kbps := strconv.Itoa(s.Bitrate) + "k"
	nvWAV := filepath.Join(dir, "no_vocals.wav")
	names := append(append([]string{}, Parts...), "no_vocals")
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	for i, p := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			src := filepath.Join(parts, p+".wav")
			if p == "no_vocals" {
				// The others summed as they are (no level change: they
				// add back up to the recording).
				if err := run(ctx, "ffmpeg", "-nostdin", "-v", "error",
					"-i", filepath.Join(parts, "drums.wav"), "-i", filepath.Join(parts, "bass.wav"), "-i", filepath.Join(parts, "other.wav"),
					"-filter_complex", "amix=inputs=3:normalize=0", nvWAV); err != nil {
					errs[i] = fmt.Errorf("mixing no_vocals: %w", err)
					return
				}
				src = nvWAV
			}
			if err := run(ctx, "ffmpeg", "-nostdin", "-v", "error", "-i", src, "-b:a", kbps, filepath.Join(dir, p+".mp3")); err != nil {
				errs[i] = fmt.Errorf("encoding %s: %w", p, err)
			}
		}()
	}
	wg.Wait()
	result := map[string][]byte{}
	for i, p := range names {
		if errs[i] != nil {
			return nil, WAVs{}, errs[i]
		}
		var err error
		if result[p], err = os.ReadFile(filepath.Join(dir, p+".mp3")); err != nil {
			return nil, WAVs{}, err
		}
	}
	return result, WAVs{Mix: wav, Backing: nvWAV, Vocals: filepath.Join(parts, "vocals.wav")}, nil
}

// threadsEnv is the environment that holds PyTorch (OpenMP, MKL) to n
// threads; nil (the worker's own) for n = 0.
func threadsEnv(n int) []string {
	if n <= 0 {
		return nil
	}
	v := strconv.Itoa(n)
	return []string{"OMP_NUM_THREADS=" + v, "MKL_NUM_THREADS=" + v}
}

// DecodeWAVs writes a stored recording and its parts as WAVs in dir, for
// analysing a recording split earlier (the analysis jobs, the backfill).
// backing and vocals may be nil.
func DecodeWAVs(ctx context.Context, dir string, mix, backing, vocals []byte) (WAVs, error) {
	var w WAVs
	write := func(name string, data []byte, to *string) error {
		if len(data) == 0 {
			return nil
		}
		in := filepath.Join(dir, name+".in")
		if err := os.WriteFile(in, data, 0o600); err != nil {
			return err
		}
		out := filepath.Join(dir, name+".wav")
		if err := run(ctx, "ffmpeg", "-nostdin", "-v", "error", "-i", in, "-ac", "2", "-ar", "44100", out); err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		*to = out
		return os.Remove(in)
	}
	if err := write("track", mix, &w.Mix); err != nil {
		return w, err
	}
	if err := write("no_vocals", backing, &w.Backing); err != nil {
		return w, err
	}
	if err := write("vocals", vocals, &w.Vocals); err != nil {
		return w, err
	}
	return w, nil
}

func run(ctx context.Context, name string, args ...string) error {
	return runEnv(ctx, nil, name, args...)
}

// runEnv runs a command with env added to the worker's environment.
func runEnv(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if len(msg) > 2000 {
			msg = msg[len(msg)-2000:]
		}
		return fmt.Errorf("%v: %s", err, msg)
	}
	return nil
}
