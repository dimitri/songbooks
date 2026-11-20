package stems

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"app/internal/audioanalysis"
	"app/internal/chartalign"
)

// Analyzer listens to a split recording: its beats, downbeats and chords
// (deploy/analysis/sbanalyze.py: Beat This! on the backing part,
// lv-chordia on the mix), then the voice, the key and the audio's start
// with the app's own analysis (internal/audioanalysis), summarised by
// chartalign.FromRaw. What the stems worker stores per recording
// (song_audio_analysis).
type Analyzer struct {
	// Command runs sbanalyze.py: "python /opt/sbanalyze/sbanalyze.py".
	Command []string
	// Threads PyTorch may use, 0 for its default.
	Threads int
}

// WAVs are a recording's files as the analysis reads them. Backing and
// Vocals may be "" (not split): the beats then come from the mix, and
// there's no voice.
type WAVs struct {
	Mix, Backing, Vocals string
}

// Analyze runs the models on the files, then the Go analysis.
func (a Analyzer) Analyze(ctx context.Context, w WAVs) (*chartalign.Recording, error) {
	raw, err := a.Raw(ctx, w)
	if err != nil {
		return nil, err
	}
	return Summarize(ctx, raw, w)
}

// Raw runs sbanalyze.py alone.
func (a Analyzer) Raw(ctx context.Context, w WAVs) (chartalign.Raw, error) {
	var raw chartalign.Raw
	if len(a.Command) == 0 {
		return raw, fmt.Errorf("no analysis command")
	}
	args := append([]string{}, a.Command[1:]...)
	args = append(args, "--mix", w.Mix)
	if w.Backing != "" {
		args = append(args, "--backing", w.Backing)
	}
	if a.Threads > 0 {
		args = append(args, "--threads", strconv.Itoa(a.Threads))
	}
	cmd := exec.CommandContext(ctx, a.Command[0], args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if len(msg) > 2000 {
			msg = msg[len(msg)-2000:]
		}
		return raw, fmt.Errorf("sbanalyze: %v: %s", err, msg)
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return raw, fmt.Errorf("sbanalyze's output: %w", err)
	}
	return raw, nil
}

// Summarize adds the Go analysis of the same files to the models' output.
func Summarize(ctx context.Context, raw chartalign.Raw, w WAVs) (*chartalign.Recording, error) {
	read := func(path string) ([]float32, error) {
		if path == "" {
			return nil, nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return audioanalysis.Decode(ctx, b)
	}
	mix, err := read(w.Mix)
	if err != nil {
		return nil, fmt.Errorf("mix: %w", err)
	}
	backing, err := read(w.Backing)
	if err != nil {
		return nil, fmt.Errorf("backing: %w", err)
	}
	vocals, err := read(w.Vocals)
	if err != nil {
		return nil, fmt.Errorf("vocals: %w", err)
	}
	an := audioanalysis.AnalyzeSamples(mix, vocals, backing)
	x := chartalign.Extra{AudioStart: an.AudioStart, Key: an.Key, KeyConf: an.KeyConfidence, TimeSig: an.TimeSig}
	if an.VocalActivity != nil {
		c := audioanalysis.VoiceCuesOf(an.VocalActivity, an.Hop)
		x.VoiceOnsets, x.VoicePhrases = c.Onsets, c.Phrases
	}
	return chartalign.FromRaw(raw, x), nil
}
