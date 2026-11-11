// Package webassets embeds every template and static asset the server
// serves, so `go build` alone produces a deployable single binary — no
// separate asset-build step, no files read from disk at runtime.
package webassets

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"io"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"app/internal/chorddiagram"
	"app/internal/chordsym"
	"app/internal/song"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFSRaw embed.FS

var funcMap = template.FuncMap{
	// chord renders one chord symbol in chart style (bold root, raised
	// quality, triangle for maj7, real flat/sharp glyphs) — see
	// internal/chordsym. Empty/"%"/"N.C." tokens render as plain text.
	"chord": func(sym string) template.HTML { return template.HTML(chordsym.Render(sym)) },
	// chordJazz is chord's iReal Pro counterpart (jazz chart mode only —
	// see chart-bar, play.html, which renders both and lets CSS pick per
	// data-render-mode rather than threading a mode flag through every
	// shared template): minor as "-", half-diminished spelled out
	// literally ("-7♭5") instead of the ø glyph — see RenderJazz.
	"chordJazz": func(sym string) template.HTML { return template.HTML(chordsym.RenderJazz(sym)) },
	// chordDiagram renders one "chord:" header line (ref.Name/ref.Spec —
	// see song.ChordRef) as a standalone SVG fretboard box for the
	// chart's legend row, e.g. a song page's chord-diagram strip under
	// the title. The name label is plain text (SVG <text> can't hold
	// chordsym's HTML spans), escaped by the same glyph substitution
	// chordsym itself does for "b"/"#" so e.g. "Ab" prints as "A♭".
	"chordDiagram": func(ref song.ChordRef) template.HTML {
		d := chorddiagram.ParseSpec(ref.Spec)
		return template.HTML(chorddiagram.RenderSVG(d, chordsym.GlyphName(ref.Name)))
	},
	"add1":      func(n int) int { return n + 1 },
	// navGlobal returns the global nav's declared entry list for one
	// surface ("desktop"/"mobileTab"/"mobileMore") — see nav.go, the
	// single source of truth desktop-sidebar/global-tabbar/
	// site-nav-fullpage all render from instead of each hardcoding its
	// own list.
	"navGlobal": navGlobal,
	// now gives a template a current timestamp to compare against —
	// e.g. the admin panel's invite list, which needs "is this expiry
	// in the future" rather than any value computed once at render time
	// being stale by the time someone actually reads the page.
	"now": func() time.Time { return time.Now() },
	// seq returns [0, n) for templates to range over a plain bar count —
	// e.g. the repeat-bar mockup's N empty bar cells, where N varies per
	// song (RepeatSegmentBars), not a fixed 8.
	"seq": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	},
	// label strips Typst's backslash-escaping of its special characters
	// (e.g. "Instrumental \& Solo") from section labels for display — the
	// stored text keeps the escape so the Typst pipeline still works.
	"label": unescapeTypst,
	// deref64 renders a *int64 (nil = absent) as its decimal value or "" —
	// catalog/revision templates' forked_from_id/owner_person_id display,
	// same "text/template won't dereference a pointer for you" reason as
	// maxcap above.
	"deref64": func(n *int64) string {
		if n == nil {
			return ""
		}
		return strconv.FormatInt(*n, 10)
	},
	// instrumentIcon classifies a free-text instrument string into one of
	// a handful of short category keys ("drums"/"bass"/"guitar"/"vocals"/
	// "keys"/"other"). It does NOT return a template name: Go's
	// html/template "template" action only accepts a string LITERAL as
	// its name, never a computed/variable one (confirmed — {{template $n
	// .}} fails to parse with "unexpected $n in template clause"), so a
	// dynamic-dispatch-by-name approach doesn't work here. Instead the
	// shared "instrument-icon" template in icons.html takes this key and
	// branches to a fixed {{template "icon-instrument-X"}} call per case
	// — see that template for the actual icon selection.
	//
	// Replaces the earlier plain-Unicode/emoji glyphs (🥁🪕🎸🎤), which
	// rendered inconsistently across platforms/fonts; self-hosted inline
	// SVG keeps the "no new build step/asset pipeline, no network font"
	// property emoji had, just drawn in this app's own icon style.
	//
	// Matching is by substring, case-insensitive, not an exact-value
	// switch: instrument is genuinely free text (see migration 0018 — no
	// fixed enum), and this app already has two different naming
	// conventions in real use for the same instruments — the event-signup
	// vocabulary ("drummer"/"bassist"/"guitarist"/"singer") and plain
	// band-roster nouns ("drums"/"bass"/"guitar"/"singer", "guitar &
	// singer", "guitar/bass/singer") — a strict switch would silently
	// fall through to "other" for most real band rosters.
	"instrumentIcon": func(instrument string) string {
		v := strings.ToLower(instrument)
		switch {
		case strings.Contains(v, "drum"):
			return "drums"
		case strings.Contains(v, "bass"):
			return "bass"
		case strings.Contains(v, "guitar"), strings.Contains(v, "solo"), strings.Contains(v, "rythmique"):
			return "guitar"
		case strings.Contains(v, "sing") || strings.Contains(v, "vocal") || strings.Contains(v, "chant"):
			return "vocals"
		case strings.Contains(v, "key") || strings.Contains(v, "piano"):
			return "keys"
		default:
			return "other"
		}
	},
	// contains reports whether a string slice holds v — e.g. "is this
	// instrument already checked on the person's profile."
	"contains": func(list []string, v string) bool {
		for _, s := range list {
			if s == v {
				return true
			}
		}
		return false
	},
	// autolink turns bare http(s) URLs in server-generated text (e.g. a
	// mailer.ConsoleMailer outbox body, which embeds the magic-link
	// URL) into clickable anchors, for GET /dev/outbox. The body is
	// always ours (never user-submitted), so this is safe to mark HTML.
	"autolink": func(s string) template.HTML {
		escaped := template.HTMLEscapeString(s)
		linked := outboxURLPattern.ReplaceAllString(escaped, `<a href="$1">$1</a>`)
		return template.HTML(linked)
	},
	// staticVersion is a content hash of every embedded static/ file,
	// appended as a "?v=" query param to every /static/ URL so a browser's
	// cache key changes exactly when the content does — without it,
	// default heuristic caching serves a stale app.css/song.js
	// indefinitely after a deploy (the route has no filename hashing and
	// previously set no Cache-Control at all). Computed once at startup,
	// not per-request.
	"staticVersion": func() string { return staticVersion },
}

var outboxURLPattern = regexp.MustCompile(`(https?://[^\s<]+)`)

var typstEscapes = strings.NewReplacer(`\&`, "&", `\%`, "%", `\#`, "#", `\_`, "_", `\*`, "*")

func unescapeTypst(s string) string { return typstEscapes.Replace(s) }

// Templates is every *.html template under templates/, parsed once at
// startup.
var Templates = template.Must(template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html"))

// StaticFS serves static/css, static/js etc. rooted at "/".
func StaticFS() fs.FS {
	sub, err := fs.Sub(staticFSRaw, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

// staticVersion is a short hex hash of every embedded static/ file's
// content, computed once at package init. Deterministic across identical
// builds, changes whenever any static asset's bytes change.
var staticVersion = computeStaticVersion()

// StaticVersion exposes staticVersion to Go code outside this package
// that needs the same cache-busting "?v=" suffix a template gets for
// free via the "staticVersion" template func — e.g. a handler building
// a small standalone HTML document itself rather than through a
// template (see handleDocsSongFormatExample, internal/httpapi).
func StaticVersion() string { return staticVersion }

func computeStaticVersion() string {
	var paths []string
	err := fs.WalkDir(staticFSRaw, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		f, err := staticFSRaw.Open(p)
		if err != nil {
			panic(err)
		}
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			panic(err)
		}
		f.Close()
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
