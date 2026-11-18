package audioanalysis

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"time"
)

// SampleRate is the rate every analysis in this package works at.
// 11025 Hz keeps everything up to ~5.5 kHz, which covers the
// fundamentals and low harmonics that key detection reads (60 Hz–2 kHz)
// and the attack transients the onset envelope needs, at a quarter of
// the cost of 44.1 kHz.
const SampleRate = 11025

// DecodeTimeout bounds a single ffmpeg run, matching the transcode
// ceiling used elsewhere in the app (internal/audiotranscode).
const DecodeTimeout = 3 * time.Minute

// Decode runs ffmpeg (on PATH) to turn any audio file's bytes into mono
// float32 samples at SampleRate, downmixing all channels.
//
// The input goes through a temporary file rather than stdin: MP4/M4A
// containers frequently keep their index (the moov atom) at the end of
// the file, which ffmpeg can only reach by seeking, and a pipe cannot
// seek. The file is removed before Decode returns. Output still streams
// back over stdout as raw little-endian float32 (-f f32le), so there is
// no output file to clean up.
func Decode(ctx context.Context, data []byte) ([]float32, error) {
	if len(data) == 0 {
		return nil, errors.New("audioanalysis: no audio data")
	}
	f, err := os.CreateTemp("", "audioanalysis-*")
	if err != nil {
		return nil, fmt.Errorf("audioanalysis: temp file: %w", err)
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return nil, fmt.Errorf("audioanalysis: temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("audioanalysis: temp file: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, DecodeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-i", path,
		"-vn", "-ac", "1", "-ar", fmt.Sprint(SampleRate),
		"-f", "f32le", "-acodec", "pcm_f32le", "pipe:1")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("audioanalysis: ffmpeg decode: %w: %s", err, trimOutput(stderr.Bytes()))
	}
	raw := out.Bytes()
	samples := make([]float32, len(raw)/4)
	for i := range samples {
		samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
	}
	if len(samples) == 0 {
		return nil, errors.New("audioanalysis: ffmpeg decoded no audio")
	}
	return samples, nil
}

func trimOutput(b []byte) string {
	const max = 500
	if len(b) > max {
		return string(b[len(b)-max:])
	}
	return string(b)
}
