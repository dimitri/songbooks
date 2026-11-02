// Package pdf bridges to the existing Typst print pipeline (v2/lib) to
// render a song or a setlist to PDF on demand. It shells out to the real
// `typst` CLI against v2's own templates — no native Go engraving, no
// HTML-to-PDF — and never touches v2/lib's rendering code itself.
//
// Typst's file reads are sandboxed to its --root: a "/"-prefixed input path
// is resolved relative to --root, and nothing outside that tree is
// reachable at all. So every scratch file this package writes lives
// physically under v2Root (as a .export-tmp-<random> directory, cleaned up
// unconditionally via defer) rather than under the OS temp directory.
package pdf

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"app/internal/song"
)

// Options controls the page target/theme passed through to render.typ /
// render-book.typ, same inputs the normal `make` pipeline uses.
type Options struct {
	Target string // e.g. "a4"; empty defaults to "a4"
	Theme  string // "light" or "dark"; empty defaults to "light"
}

func (o Options) target() string {
	if o.Target == "" {
		return "a4"
	}
	return o.Target
}

func (o Options) theme() string {
	if o.Theme == "" {
		return "light"
	}
	return o.Theme
}

// Bridge knows where the v2 Typst pipeline lives.
type Bridge struct {
	V2Root string // e.g. /Users/dim/dev/songbooks/v2
}

func New(v2Root string) *Bridge {
	return &Bridge{V2Root: v2Root}
}

func (b *Bridge) fontDir() string {
	dir := filepath.Join(b.V2Root, "fonts")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// scratch creates a fresh scratch directory physically under v2Root (so
// Typst's root-sandboxed reads can reach it) and returns its absolute path,
// its path relative to v2Root (for building "/"-prefixed Typst inputs), and
// a cleanup func the caller must defer unconditionally.
func (b *Bridge) scratch() (abs, rel string, cleanup func(), err error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", "", nil, err
	}
	name := ".export-tmp-" + hex.EncodeToString(buf[:])
	abs = filepath.Join(b.V2Root, name)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", "", nil, err
	}
	return abs, "/" + name, func() { os.RemoveAll(abs) }, nil
}

func (b *Bridge) typstFlags() []string {
	flags := []string{"--root", b.V2Root}
	if fd := b.fontDir(); fd != "" {
		flags = append(flags, "--font-path", fd)
	}
	return flags
}

// fitInfo mirrors the <fit> metadata block sheet.typ exposes — only `size`
// is actually read back by fit-one-page when a fit dict is handed in, so
// that's the only field this package round-trips.
type fitInfo struct {
	Size float64 `json:"size"`
}

var pagesRE = regexp.MustCompile(`(?m)^Pages:\s*(\d+)`)

// fitOnePage runs the same bisection loop v2/tools/compile-sheet.sh uses for
// the normal `make` pipeline (compile, check the real page count via
// pdfinfo, lower the ceiling 6% and retry, up to 8 iterations, accept the
// floor otherwise — an editorial signal about the song, not a layout bug to
// paper over), against a song already materialized at songAbsPath (which
// must live under scratchAbs, itself under v2Root). Returns the final
// one-page (or best-effort) PDF bytes and the fit size that produced them.
func (b *Bridge) fitOnePage(ctx context.Context, songRootRel string, target, theme string) ([]byte, fitInfo, error) {
	var ceiling string
	var pdfBytes []byte
	var fit fitInfo
	for i := 0; i < 8; i++ {
		out, err := os.CreateTemp("", "songbooks-sheet-*.pdf")
		if err != nil {
			return nil, fitInfo{}, err
		}
		out.Close()
		defer os.Remove(out.Name())

		args := append([]string{"compile"}, b.typstFlags()...)
		args = append(args,
			"--input", "song="+songRootRel,
			"--input", "target="+target,
			"--input", "theme="+theme,
		)
		if ceiling != "" {
			args = append(args, "--input", "ceiling="+ceiling)
		}
		args = append(args, "--diagnostic-format", "short",
			filepath.Join(b.V2Root, "render.typ"), out.Name())

		if err := runTypst(ctx, args); err != nil {
			return nil, fitInfo{}, err
		}

		data, err := os.ReadFile(out.Name())
		if err != nil {
			return nil, fitInfo{}, err
		}
		pdfBytes = data

		pages, err := pageCount(out.Name())
		if err != nil {
			return nil, fitInfo{}, err
		}

		f, err := queryFit(ctx, b, songRootRel, target, theme, ceiling)
		if err != nil {
			return nil, fitInfo{}, err
		}
		fit = f

		if pages == 1 {
			return pdfBytes, fit, nil
		}
		if fit.Size <= 0 {
			break
		}
		ceiling = strconv.FormatFloat(fit.Size*0.94, 'f', 2, 64)
	}
	// Gave up at the floor, same as compile-sheet.sh: return the best
	// attempt rather than failing the whole export over one long song.
	return pdfBytes, fit, nil
}

func queryFit(ctx context.Context, b *Bridge, songRootRel, target, theme, ceiling string) (fitInfo, error) {
	args := append([]string{"eval"}, b.typstFlags()...)
	args = append(args, "--in", filepath.Join(b.V2Root, "render.typ"))
	args = append(args, "--input", "song="+songRootRel,
		"--input", "target="+target, "--input", "theme="+theme)
	if ceiling != "" {
		args = append(args, "--input", "ceiling="+ceiling)
	}
	args = append(args, "--format", "json", "query(<fit>).first().value")

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "typst", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fitInfo{}, fmt.Errorf("typst eval: %w: %s", err, stderr.String())
	}
	var fit fitInfo
	if err := json.Unmarshal(stdout.Bytes(), &fit); err != nil {
		return fitInfo{}, fmt.Errorf("parsing fit info: %w", err)
	}
	return fit, nil
}

func runTypst(ctx context.Context, args []string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "typst", args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("typst %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return nil
}

func pageCount(pdfPath string) (int, error) {
	var stdout bytes.Buffer
	cmd := exec.Command("pdfinfo", pdfPath)
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("pdfinfo: %w", err)
	}
	m := pagesRE.FindStringSubmatch(stdout.String())
	if m == nil {
		return 0, fmt.Errorf("pdfinfo: could not find page count")
	}
	n, err := strconv.Atoi(m[1])
	return n, err
}

// copyScores copies every "score:"-referenced SVG a song's body names from
// the originating project's own <v2Root>/<book>/scores/ directory into the
// scratch dir's sibling scores/ directory — lib/song.typ's parse-song
// resolves a score's path as "<two-dirs-up-from-the-song-file>/scores/
// <name>", so the scratch songs/ dir needs a scores/ sibling with the same
// files the real project ships, or sheet.typ fails with "file not found".
// Known limitation, not silently papered over: if two source projects in
// the same setlist export both reference a DIFFERENT score file with the
// SAME filename, the second copy wins — acceptable for a personal-project
// scale export, not fixed here.
func copyScores(v2Root, book, body, scoresDir string) error {
	parsed := song.Parse(body)
	var names []string
	for _, sec := range parsed.Sections {
		if sec.Score != "" {
			names = append(names, sec.Score)
		}
	}
	if len(names) == 0 {
		return nil
	}
	if err := os.MkdirAll(scoresDir, 0o755); err != nil {
		return err
	}
	for _, name := range names {
		src := filepath.Join(v2Root, book, "scores", name)
		dst := filepath.Join(scoresDir, name)
		in, err := os.Open(src)
		if err != nil {
			return fmt.Errorf("score %q referenced by a song in %s but not found at %s: %w", name, book, src, err)
		}
		out, err := os.Create(dst)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// ExportSong renders one song's body text to a one-page (best-effort) PDF.
func (b *Bridge) ExportSong(ctx context.Context, book, body string, opts Options) ([]byte, error) {
	abs, rel, cleanup, err := b.scratch()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	songsDir := filepath.Join(abs, "songs")
	if err := os.MkdirAll(songsDir, 0o755); err != nil {
		return nil, err
	}
	songPath := filepath.Join(songsDir, "export.song")
	if err := os.WriteFile(songPath, []byte(body), 0o644); err != nil {
		return nil, err
	}
	if err := copyScores(b.V2Root, book, body, filepath.Join(abs, "scores")); err != nil {
		return nil, err
	}

	pdfBytes, _, err := b.fitOnePage(ctx, rel+"/songs/export.song", opts.target(), opts.theme())
	return pdfBytes, err
}

// SongRef is one resolved setlist entry, ready to materialize.
type SongRef struct {
	Group string // the containing Set's label, e.g. "Set 1" — "" groups as "Songs"
	Book  string
	Stem  string
	Body  string
}

// FrontMatter controls the cover + title page rendered before the
// contents/setlist table. CoverTitle defaults to Title when empty. An
// EventTitle (or a non-empty Date/Venue) selects the event-aware title
// page (event-title-plate); with all three empty, a plain title-plate is
// used instead — see render-book.typ's own doc comment for the exact
// selection rule.
type FrontMatter struct {
	CoverTitle string
	EventTitle string
	BandName   string
	Date       string
	Venue      string
}

var slugSanitizeRE = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// ExportSetlist renders every entry to a single concatenated PDF (a cover
// page, a title page, a contents/setlist page, then one page per song),
// grouped the same way internal/store/order.go's SaveOrder writes
// "# SET: Label" groups.
func (b *Bridge) ExportSetlist(ctx context.Context, title string, front FrontMatter, entries []SongRef, opts Options) ([]byte, error) {
	abs, rel, cleanup, err := b.scratch()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	songsDir := filepath.Join(abs, "songs")
	if err := os.MkdirAll(songsDir, 0o755); err != nil {
		return nil, err
	}

	fits := map[string]fitInfo{}
	type groupedSlug struct {
		group string
		slug  string
	}
	var ordered []groupedSlug

	for i, e := range entries {
		slug := fmt.Sprintf("entry%02d-%s", i, slugSanitizeRE.ReplaceAllString(e.Book+"-"+e.Stem, "-"))
		songPath := filepath.Join(songsDir, slug+".song")
		if err := os.WriteFile(songPath, []byte(e.Body), 0o644); err != nil {
			return nil, err
		}
		if err := copyScores(b.V2Root, e.Book, e.Body, filepath.Join(abs, "scores")); err != nil {
			return nil, err
		}
		_, fit, err := b.fitOnePage(ctx, rel+"/songs/"+slug+".song", opts.target(), opts.theme())
		if err != nil {
			return nil, fmt.Errorf("rendering %s/%s: %w", e.Book, e.Stem, err)
		}
		fits[slug] = fit
		ordered = append(ordered, groupedSlug{group: e.Group, slug: slug})
	}

	// order.txt: "# SET: Label" markers, one stem per line — same shape
	// internal/store/order.go's SaveOrder writes, with a lone "Songs"
	// group left unmarked.
	var orderTxt strings.Builder
	var lastGroup string
	first := true
	for _, g := range ordered {
		label := g.group
		if label == "" {
			label = "Songs"
		}
		if first || label != lastGroup {
			if !first {
				orderTxt.WriteString("\n")
			}
			if label != "Songs" {
				orderTxt.WriteString("# SET: " + label + "\n")
			}
			lastGroup = label
			first = false
		}
		orderTxt.WriteString(g.slug + "\n")
	}
	if err := os.WriteFile(filepath.Join(abs, "order.txt"), []byte(orderTxt.String()), 0o644); err != nil {
		return nil, err
	}

	fitsJSON, err := json.Marshal(fits)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(abs, "fits.json"), fitsJSON, 0o644); err != nil {
		return nil, err
	}

	out, err := os.CreateTemp("", "songbooks-book-*.pdf")
	if err != nil {
		return nil, err
	}
	out.Close()
	defer os.Remove(out.Name())

	coverTitle := front.CoverTitle
	if coverTitle == "" {
		coverTitle = title
	}

	args := append([]string{"compile"}, b.typstFlags()...)
	args = append(args,
		"--input", "order-file="+rel+"/order.txt",
		"--input", "songdir="+rel+"/songs",
		"--input", "fits-file="+rel+"/fits.json",
		"--input", "target="+opts.target(),
		"--input", "theme="+opts.theme(),
		"--input", "title="+title,
		"--input", "cover-title="+coverTitle,
		"--input", "event-title="+front.EventTitle,
		"--input", "band-name="+front.BandName,
		"--input", "event-date="+front.Date,
		"--input", "event-venue="+front.Venue,
		"--diagnostic-format", "short",
		filepath.Join(b.V2Root, "render-book.typ"), out.Name())

	if err := runTypst(ctx, args); err != nil {
		return nil, err
	}
	return os.ReadFile(out.Name())
}

// ExportSongbook renders an entire songbook — a project's full song list,
// or a band's full repertoire — using the exact same cover/title/contents/
// songs structure as ExportSetlist, just fed every song with no Set
// grouping (SongRef.Group left "" throughout: a songbook isn't divided
// into performance sets) and no event metadata (a songbook isn't tied to
// one gig, so it always gets the plain title-plate, not an event page).
func (b *Bridge) ExportSongbook(ctx context.Context, title string, entries []SongRef, opts Options) ([]byte, error) {
	return b.ExportSetlist(ctx, title, FrontMatter{}, entries, opts)
}

// ExportSetlistPageOnly renders just the running-order table (#, Title,
// Key, Tempo, grouped by set) as a single-page PDF — no song pages, far
// cheaper than ExportSetlist since it never materializes/fits each song's
// full chart. subtitle is typically the event's date/venue, or "" to omit
// the subtitle line entirely.
func (b *Bridge) ExportSetlistPageOnly(ctx context.Context, title, subtitle string, entries []SongRef, opts Options) ([]byte, error) {
	abs, rel, cleanup, err := b.scratch()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	songsDir := filepath.Join(abs, "songs")
	if err := os.MkdirAll(songsDir, 0o755); err != nil {
		return nil, err
	}

	var orderTxt strings.Builder
	var lastGroup string
	first := true
	for i, e := range entries {
		slug := fmt.Sprintf("entry%02d-%s", i, slugSanitizeRE.ReplaceAllString(e.Book+"-"+e.Stem, "-"))
		songPath := filepath.Join(songsDir, slug+".song")
		if err := os.WriteFile(songPath, []byte(e.Body), 0o644); err != nil {
			return nil, err
		}
		label := e.Group
		if label == "" {
			label = "Songs"
		}
		if first || label != lastGroup {
			if !first {
				orderTxt.WriteString("\n")
			}
			if label != "Songs" {
				orderTxt.WriteString("# SET: " + label + "\n")
			}
			lastGroup = label
			first = false
		}
		orderTxt.WriteString(slug + "\n")
	}
	if err := os.WriteFile(filepath.Join(abs, "order.txt"), []byte(orderTxt.String()), 0o644); err != nil {
		return nil, err
	}

	out, err := os.CreateTemp("", "songbooks-setlist-page-*.pdf")
	if err != nil {
		return nil, err
	}
	out.Close()
	defer os.Remove(out.Name())

	args := append([]string{"compile"}, b.typstFlags()...)
	args = append(args,
		"--input", "order-file="+rel+"/order.txt",
		"--input", "songdir="+rel+"/songs",
		"--input", "target="+opts.target(),
		"--input", "theme="+opts.theme(),
		"--input", "title="+title,
		"--input", "subtitle="+subtitle,
		"--diagnostic-format", "short",
		filepath.Join(b.V2Root, "render-setlist-page.typ"), out.Name())

	if err := runTypst(ctx, args); err != nil {
		return nil, err
	}
	return os.ReadFile(out.Name())
}
