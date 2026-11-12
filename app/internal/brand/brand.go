// Package brand is a community's or band's look: colors, fonts, the
// shape of buttons, a logo mark, a tagline and a website link. It is
// stored as JSON on the community or band (communities.brand,
// bands.brand) and applied to every page seen in that community's or
// band's context — its own pages, its songbooks and setlists, and the
// songs opened from them — by overriding app.css's own design tokens
// (--bg, --fg, --accent, --font-ui...) on <body>. Nothing else in the
// stylesheet changes, so a brand can't break a layout, only recolor it.
//
// Fonts come from a short curated list served by Google Fonts, and every
// color must be a hex color: a brand is data a band member types in, so
// nothing from it reaches the page as raw CSS or as an arbitrary URL.
package brand

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Brand is what's stored: a preset and the fields that override it. Empty
// fields take the preset's value, or the app's own when there's no preset.
type Brand struct {
	Preset      string `json:"preset,omitempty"`
	Background  string `json:"background,omitempty"` // page background
	Surface     string `json:"surface,omitempty"`    // cards, panels
	Text        string `json:"text,omitempty"`
	Muted       string `json:"muted,omitempty"` // secondary text
	Accent      string `json:"accent,omitempty"`
	Accent2     string `json:"accent2,omitempty"` // a second accent for small touches (eyebrows, tags)
	Header      string `json:"header,omitempty"`  // the sidebar / header band
	FontDisplay string `json:"fontDisplay,omitempty"`
	FontUI      string `json:"fontUI,omitempty"`
	Shape       string `json:"shape,omitempty"` // square, soft or pill
	Mark        string `json:"mark,omitempty"`  // a short logo mark: a letter, a symbol (☾), an emoji
	LogoURL     string `json:"logoURL,omitempty"`
	Tagline     string `json:"tagline,omitempty"`
	Website     string `json:"website,omitempty"`
}

// Font is one choice of the curated list: its CSS stack and the Google
// Fonts family spec that loads it ("" for a font the app already has).
type Font struct {
	Label  string
	Stack  string
	Google string
}

// Fonts is the curated list a brand picks from, by key.
var Fonts = map[string]Font{
	"system":        {"System (the app's default)", `system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`, ""},
	"assistant":     {"Assistant", `"Assistant", "Helvetica Neue", Arial, sans-serif`, "Assistant:wght@400;600;700"},
	"inter":         {"Inter", `"Inter", "Helvetica Neue", Arial, sans-serif`, "Inter:wght@400;500;600;700"},
	"space-grotesk": {"Space Grotesk", `"Space Grotesk", "Helvetica Neue", Arial, sans-serif`, "Space+Grotesk:wght@400;500;700"},
	"work-sans":     {"Work Sans", `"Work Sans", "Helvetica Neue", Arial, sans-serif`, "Work+Sans:wght@400;500;600;700"},
	"oswald":        {"Oswald", `"Oswald", "Arial Narrow", sans-serif`, "Oswald:wght@400;500;600;700"},
	"fraunces":      {"Fraunces", `"Fraunces", Georgia, "Times New Roman", serif`, "Fraunces:ital,wght@0,400;0,600;0,700;1,400"},
	"playfair":      {"Playfair Display", `"Playfair Display", Georgia, serif`, "Playfair+Display:ital,wght@0,400;0,700;1,400"},
	"eb-garamond":   {"EB Garamond", `"EB Garamond", Georgia, serif`, ""},
	"bebas":         {"Bebas Neue", `"Bebas Neue", "Arial Narrow", sans-serif`, "Bebas+Neue"},
}

// FontKeys is Fonts' keys in label order, for a picker.
func FontKeys() []string {
	keys := make([]string, 0, len(Fonts))
	for k := range Fonts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return Fonts[keys[i]].Label < Fonts[keys[j]].Label })
	return keys
}

// Preset is a ready-made look a community or band starts from.
type Preset struct {
	Label string
	Brand Brand
}

// Presets are the looks offered as starting points. The first two follow
// the websites of the community and band they were made for.
var Presets = map[string]Preset{
	// florentpassamonti.fr: black header, white pages, Assistant, square
	// black buttons, the warm red of his blues-rock covers.
	"florent-passamonti": {"Blues & Rock (black and white, square)", Brand{
		Background: "#ffffff", Surface: "#f3f3f3", Text: "#121212", Muted: "#5c5c5c",
		Accent: "#121212", Accent2: "#c8281e", Header: "#000000",
		FontDisplay: "assistant", FontUI: "assistant", Shape: "square",
	}},
	// otherlies.fr: night-blue ink, ice-blue and rose, Fraunces over
	// Space Grotesk, pill buttons, a crescent moon.
	"other-lies": {"Night (ink blue, ice and rose, pills)", Brand{
		Background: "#0e141b", Surface: "#131a22", Text: "#f4f8fb", Muted: "#9fb2c2",
		Accent: "#8fd0f2", Accent2: "#f27c8e", Header: "#0e141b",
		FontDisplay: "fraunces", FontUI: "space-grotesk", Shape: "pill", Mark: "☾",
	}},
	"stage": {"Stage (dark, warm amber)", Brand{
		Background: "#141210", Surface: "#1e1b18", Text: "#f2ece4", Muted: "#a89f94",
		Accent: "#e0a43a", Accent2: "#d4573b", Header: "#0b0a09",
		FontDisplay: "oswald", FontUI: "inter", Shape: "soft",
	}},
	"paper": {"Paper (cream, ink, serif titles)", Brand{
		Background: "#faf6ee", Surface: "#ffffff", Text: "#24201b", Muted: "#6f665b",
		Accent: "#2f5d8a", Accent2: "#af4e39", Header: "#24201b",
		FontDisplay: "playfair", FontUI: "work-sans", Shape: "soft",
	}},
}

// PresetKeys is Presets' keys in label order.
func PresetKeys() []string {
	keys := make([]string, 0, len(Presets))
	for k := range Presets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return Presets[keys[i]].Label < Presets[keys[j]].Label })
	return keys
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Clean checks a brand someone submitted and keeps only what's valid:
// known preset, fonts and shape, hex colors, an https logo and website,
// short texts. It never fails: an invalid field is just dropped.
func Clean(b Brand) Brand {
	if _, ok := Presets[b.Preset]; !ok {
		b.Preset = ""
	}
	for _, c := range []*string{&b.Background, &b.Surface, &b.Text, &b.Muted, &b.Accent, &b.Accent2, &b.Header} {
		*c = strings.TrimSpace(*c)
		if !hexColor.MatchString(*c) {
			*c = ""
		}
		*c = strings.ToLower(*c)
	}
	for _, f := range []*string{&b.FontDisplay, &b.FontUI} {
		if _, ok := Fonts[*f]; !ok {
			*f = ""
		}
	}
	switch b.Shape {
	case "square", "soft", "pill":
	default:
		b.Shape = ""
	}
	b.Mark = clip(strings.TrimSpace(b.Mark), 3)
	b.Tagline = clip(strings.TrimSpace(b.Tagline), 140)
	b.LogoURL = httpsURL(b.LogoURL)
	b.Website = httpsURL(b.Website)
	return b
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func httpsURL(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return u.String()
}

// Parse reads a stored brand; malformed or empty JSON is no brand.
func Parse(raw []byte) Brand {
	var b Brand
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &b)
	}
	return Clean(b)
}

// IsZero: nothing set at all, the app's own look.
func (b Brand) IsZero() bool { return b == Brand{} }

// Theme is a brand made ready for a page: the preset filled in under the
// overrides, with the CSS and font link to apply it.
type Theme struct {
	Brand
	Name     string       // the community or band, for the page banner
	Style    template.CSS // custom properties for <body style>
	FontsURL string       // a Google Fonts stylesheet, "" when none is needed
	Dark     bool         // a dark page background
}

// WebsiteHost is the website without its scheme and slash, to show.
func (t *Theme) WebsiteHost() string {
	if u, err := url.Parse(t.Website); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Host, "www.") + strings.TrimSuffix(u.Path, "/")
	}
	return t.Website
}

// Resolve makes the Theme for a stored brand; nil for no brand.
func Resolve(b Brand, name string) *Theme {
	b = Clean(b)
	if b.IsZero() {
		return nil
	}
	full := b
	if p, ok := Presets[b.Preset]; ok {
		full = merge(p.Brand, b)
	}
	t := &Theme{Brand: full, Name: name}
	t.Dark = full.Background != "" && luminance(full.Background) < 0.35
	t.Style = template.CSS(t.vars())
	t.FontsURL = fontsURL(full.FontDisplay, full.FontUI)
	return t
}

func merge(base, over Brand) Brand {
	pick := func(a, b string) string {
		if b != "" {
			return b
		}
		return a
	}
	return Brand{
		Preset:      over.Preset,
		Background:  pick(base.Background, over.Background),
		Surface:     pick(base.Surface, over.Surface),
		Text:        pick(base.Text, over.Text),
		Muted:       pick(base.Muted, over.Muted),
		Accent:      pick(base.Accent, over.Accent),
		Accent2:     pick(base.Accent2, over.Accent2),
		Header:      pick(base.Header, over.Header),
		FontDisplay: pick(base.FontDisplay, over.FontDisplay),
		FontUI:      pick(base.FontUI, over.FontUI),
		Shape:       pick(base.Shape, over.Shape),
		Mark:        pick(base.Mark, over.Mark),
		LogoURL:     pick(base.LogoURL, over.LogoURL),
		Tagline:     pick(base.Tagline, over.Tagline),
		Website:     pick(base.Website, over.Website),
	}
}

// vars is the brand as app.css's own design tokens. Only what the brand
// sets is overridden; the rest keeps the app's values.
func (t *Theme) vars() string {
	b := t.Brand
	var v []string
	set := func(name, value string) {
		if value != "" {
			v = append(v, name+": "+value)
		}
	}
	if b.Background != "" {
		set("--bg", b.Background)
		if t.Dark {
			set("color-scheme", "dark")
		} else {
			set("color-scheme", "light")
		}
	}
	set("--bg-raised", b.Surface)
	if b.Background != "" && b.Surface != "" {
		set("--bg-sunken", mix(b.Background, b.Surface, 0.5))
		set("--border", mix(b.Background, b.Text, 0.16))
		set("--border-strong", mix(b.Background, b.Text, 0.3))
	}
	set("--fg", b.Text)
	set("--fg-muted", b.Muted)
	if b.Muted != "" && b.Background != "" {
		set("--fg-faint", mix(b.Background, b.Muted, 0.6))
	}
	if b.Accent != "" {
		set("--accent", b.Accent)
		set("--accent-ink", b.Accent)
		set("--accent-contrast", contrastOn(b.Accent))
		if b.Background != "" {
			set("--accent-bg", mix(b.Background, b.Accent, 0.16))
			set("--current-bg", mix(b.Background, b.Accent, 0.22))
		}
	}
	set("--accent-2", b.Accent2)
	if b.Header != "" {
		set("--sidebar-bg", b.Header)
		set("--sidebar-fg", contrastOn(b.Header))
		set("--sidebar-active-bg", mix(b.Header, contrastOn(b.Header), 0.14))
		set("--sidebar-active-fg", contrastOn(b.Header))
		set("--brand-header", b.Header)
		set("--brand-header-fg", contrastOn(b.Header))
	}
	if f, ok := Fonts[b.FontUI]; ok {
		set("--font-ui", f.Stack)
	}
	if f, ok := Fonts[b.FontDisplay]; ok {
		set("--font-display", f.Stack)
	}
	switch b.Shape {
	case "square":
		set("--radius", "0px")
		set("--radius-control", "0px")
	case "pill":
		set("--radius-control", "999px")
	case "soft":
		set("--radius-control", "8px")
	}
	return strings.Join(v, "; ")
}

func fontsURL(keys ...string) string {
	var fams []string
	seen := map[string]bool{}
	for _, k := range keys {
		if f, ok := Fonts[k]; ok && f.Google != "" && !seen[f.Google] {
			seen[f.Google] = true
			fams = append(fams, "family="+f.Google)
		}
	}
	if len(fams) == 0 {
		return ""
	}
	return "https://fonts.googleapis.com/css2?" + strings.Join(fams, "&") + "&display=swap"
}

func rgb(hex string) (r, g, b float64) {
	n, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	return float64(n>>16&0xff) / 255, float64(n>>8&0xff) / 255, float64(n&0xff) / 255
}

// luminance is the relative luminance (WCAG) of a hex color.
func luminance(hex string) float64 {
	r, g, b := rgb(hex)
	lin := func(c float64) float64 {
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrastOn is the text color to put on a background: near-black or
// white, whichever reads better.
func contrastOn(hex string) string {
	if luminance(hex) > 0.4 {
		return "#121212"
	}
	return "#ffffff"
}

// mix is a + (b − a)·t in RGB, as a hex color.
func mix(a, b string, t float64) string {
	ar, ag, ab := rgb(a)
	br, bg, bb := rgb(b)
	c := func(x, y float64) int { return int((x+(y-x)*t)*255 + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", c(ar, br), c(ag, bg), c(ab, bb))
}
