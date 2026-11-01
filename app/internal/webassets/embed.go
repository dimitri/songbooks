// Package webassets embeds every template and static asset the server
// serves, so `go build` alone produces a deployable single binary — no
// separate asset-build step, no files read from disk at runtime.
package webassets

import (
	"embed"
	"html/template"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"app/internal/chordsym"
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
	"add1":  func(n int) int { return n + 1 },
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
	// maxcap renders a *int headcount cap (nil = no cap set) as either its
	// number or "no cap" — jam_songs.html/jam_roster.html's own caps
	// display. Needed because text/template's default %v on a *int prints
	// its address, not the pointed-to value.
	"maxcap": func(n *int) string {
		if n == nil {
			return "no cap"
		}
		return strconv.Itoa(*n)
	},
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
	// instrumentIcon renders the shared icon-picker glyph for a common
	// instrument name — plain Unicode/emoji, not an icon font or SVG
	// sprite, so no new build step/asset pipeline is needed for it (see
	// jam_landing.html/jam_songs.html/jam_add_songs.html's icon pickers).
	// Anything outside this small set has no icon — those stay text input
	// ("other").
	"instrumentIcon": func(instrument string) string {
		switch instrument {
		case "drummer":
			return "\U0001F941" // 🥁
		case "bassist":
			return "\U0001FA95" // 🪕 (stands in for bass — distinguishable from guitarist's 🎸 at a glance)
		case "guitarist":
			return "\U0001F3B8" // 🎸
		case "singer":
			return "\U0001F3A4" // 🎤
		default:
			return ""
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
