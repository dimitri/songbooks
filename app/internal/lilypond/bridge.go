// This file is the shell-out half of package lilypond: it takes the .ly
// source lilypond.go's Render/RenderBook produce and turns it into an
// actual PDF via the `lilypond` CLI — mirroring the pattern
// internal/pdf.Bridge uses for the Typst pipeline (write scratch input,
// shell out, read the PDF back, clean up unconditionally), but simpler:
// LilyPond has no root-sandboxing to work around, so the scratch
// directory can just be an ordinary OS temp dir (os.MkdirTemp) rather
// than something that has to live inside a particular tree — nothing
// here touches the repo, so there's nothing to .gitignore either.
//
// Unlike internal/pdf's Typst bridge, this pipeline renders straight to
// PDF with the `lilypond` binary itself; no LaTeX wrapper is involved.
// LilyPond's own \score/\pageBreak/\header machinery (see RenderBook)
// already gives multi-song pagination and per-song headers, which is as
// much "book" layout control as a first version needs — a LaTeX layer
// would only earn its keep for TOC/running-header needs beyond that,
// which is future work, not this phase.
package lilypond

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"app/internal/song"
)

// Bridge shells out to the `lilypond` CLI. It carries no configuration —
// unlike internal/pdf.Bridge there's no external template tree to point
// at, since the .ly source is generated entirely in Go by this package.
type Bridge struct{}

func NewBridge() *Bridge { return &Bridge{} }

// ExportSong renders one song's body to a standalone PDF.
func (b *Bridge) ExportSong(ctx context.Context, s *song.Song) ([]byte, error) {
	return compile(ctx, Render(s))
}

// ExportBook renders every entry to one combined, paginated PDF — each
// song its own page (LilyPond's own page-fill decides if a short song
// shares a page with the next, same as a real lead-sheet book), with a
// group heading markup page break inserted whenever the group changes.
func (b *Bridge) ExportBook(ctx context.Context, title string, entries []BookEntry) ([]byte, error) {
	return compile(ctx, RenderBook(title, entries))
}

// compile writes src to a fresh scratch dir, shells out to `lilypond`,
// and returns the resulting PDF bytes. The scratch dir is removed
// unconditionally, success or failure.
func compile(ctx context.Context, src string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "songbooks-ly-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "song.ly")
	if err := os.WriteFile(inPath, []byte(src), 0o644); err != nil {
		return nil, err
	}

	outBase := filepath.Join(dir, "out")
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "lilypond",
		"-dno-point-and-click", // skip embedding file:// click-to-source links — irrelevant and leaks scratch paths into the PDF
		"-o", outBase,
		inPath,
	)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("lilypond: %w: %s", err, lastLines(stderr.String(), 20))
	}

	data, err := os.ReadFile(outBase + ".pdf")
	if err != nil {
		return nil, fmt.Errorf("lilypond reported success but %s.pdf is missing: %w", outBase, err)
	}
	return data, nil
}

// lastLines keeps error messages from this package reasonably sized —
// lilypond's own diagnostics can be long, and only the tail (the actual
// error, not the whole startup banner) is useful in an API error string.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
