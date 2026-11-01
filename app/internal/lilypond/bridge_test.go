package lilypond

import (
	"context"
	"os/exec"
	"testing"

	"app/internal/song"
)

// lilypondAvailable skips a test rather than failing the build when the
// `lilypond` binary isn't on this machine's PATH — go build/vet/test must
// stay green either way; only these live-compile tests depend on it.
func lilypondAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("lilypond"); err != nil {
		t.Skip("lilypond not installed, skipping live compile test")
	}
}

func TestBridgeExportSong(t *testing.T) {
	lilypondAvailable(t)
	s := song.Parse("title: Bridge Test\nartist: Tester\ntempo: 100\n\n[Verse]\ngrid: | C | G7 | Am | F |\n[C]Hello [G7]world\n")
	data, err := NewBridge().ExportSong(context.Background(), s)
	if err != nil {
		t.Fatalf("ExportSong: %v", err)
	}
	if len(data) < 500 || string(data[:5]) != "%PDF-" {
		t.Fatalf("doesn't look like a PDF (%d bytes)", len(data))
	}
}

func TestBridgeExportBook(t *testing.T) {
	lilypondAvailable(t)
	s1 := song.Parse("title: Song One\nartist: Artist A\n\n[Verse]\ngrid: | C | G |\n[C]La [G]la\n")
	s2 := song.Parse("title: Song Two\nartist: Artist B\n\n[Verse]\ngrid: | Dm | Am |\n[Dm]Na [Am]na\n")
	entries := []BookEntry{
		{Group: "Set 1", Song: s1},
		{Group: "Set 1", Song: s2},
	}
	data, err := NewBridge().ExportBook(context.Background(), "My Setlist", entries)
	if err != nil {
		t.Fatalf("ExportBook: %v", err)
	}
	if len(data) < 500 || string(data[:5]) != "%PDF-" {
		t.Fatalf("doesn't look like a PDF (%d bytes)", len(data))
	}
}
