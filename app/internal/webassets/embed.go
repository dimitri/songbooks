// Package webassets embeds every template and static asset the server
// serves, so `go build` alone produces a deployable single binary — no
// separate asset-build step, no files read from disk at runtime.
package webassets

import (
	"embed"
	"html/template"
	"io/fs"
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
}

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
