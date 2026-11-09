package audiotranscode

import (
	"context"
	"os"
	"testing"
)

func TestNeedsTranscode_WAV(t *testing.T) {
	data := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	if !NeedsTranscode(data) {
		t.Fatal("a WAV header should need transcoding")
	}
}

func TestNeedsTranscode_MP3(t *testing.T) {
	id3 := append([]byte("ID3\x03\x00\x00\x00\x00\x00\x00"), make([]byte, 20)...)
	if NeedsTranscode(id3) {
		t.Fatal("an ID3-tagged MP3 should not need transcoding")
	}
	frameSync := []byte{0xFF, 0xFB, 0x90, 0x00}
	frameSync = append(frameSync, make([]byte, 20)...)
	if NeedsTranscode(frameSync) {
		t.Fatal("a raw MPEG frame sync should not need transcoding")
	}
}

func TestNeedsTranscode_TooShort(t *testing.T) {
	if NeedsTranscode([]byte{1, 2, 3}) {
		t.Fatal("too-short data should be left alone, not misdetected as needing transcoding")
	}
}

// TestToMP3_RealWAV runs the real ffmpeg transcode against a real
// uncompressed WAV extracted from an actual Guitar Pro file's embedded
// backing track this session (Mottainai Structure SD.gp, 67MB) — not a
// synthetic fixture, since the whole point is confirming the real-world
// case (a GP file's rawAudioFile arriving as WAV, not MP3) is handled.
// Skipped when ffmpeg or the extracted fixture aren't available, so CI/
// other machines without either don't fail on this one integration test.
func TestToMP3_RealWAV(t *testing.T) {
	const fixture = "/tmp/wavtest/sample.wav"
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	if !NeedsTranscode(data) {
		t.Fatal("a real WAV file should be detected as needing transcoding")
	}
	mp3, err := ToMP3(context.Background(), data)
	if err != nil {
		t.Fatalf("ToMP3: %v", err)
	}
	if len(mp3) == 0 {
		t.Fatal("ToMP3 returned no bytes")
	}
	if NeedsTranscode(mp3) {
		t.Fatal("the transcoded output should itself look like a compressed MP3")
	}
	t.Logf("compressed %d bytes (WAV) -> %d bytes (MP3), %.1fx smaller", len(data), len(mp3), float64(len(data))/float64(len(mp3)))
}
