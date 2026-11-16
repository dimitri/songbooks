package brand

import (
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"
)

// Color is one color of a song's cover and how much of it there is.
type Color struct {
	Hex   string
	Share float64
}

// Song is what Derive reads of one song: its cover's colors and its genre.
type Song struct {
	Colors []Color
	Genre  string
}

// Derived is a look made from songs: the brand, and what it was read from.
type Derived struct {
	Brand
	Mood   string   // "dark" or "light": the covers' own
	Genres []string // the songs' genres, the most frequent first
	Hues   []string // the covers' main colors, strongest first, for a strip
	Hue    float64  // the accent's hue (OKLCH degrees)
}

// Fit is how well a cover goes with the look, 0 to 1: how much of it is
// near the accent's hue, and colorful.
func (d Derived) Fit(colors []Color) float64 {
	var f float64
	for _, c := range colors {
		if !hexColor.MatchString(c.Hex) {
			continue
		}
		_, ch, h := oklch(c.Hex)
		if near := 1 - hueDist(h, d.Hue)/60; near > 0 {
			f += c.Share * near * math.Min(1, ch/0.12)
		}
	}
	return f
}

// HueStyles is Hues as a style each, for a strip of swatches.
func (d Derived) HueStyles() []template.CSS {
	var out []template.CSS
	for _, h := range d.Hues {
		if hexColor.MatchString(h) {
			out = append(out, template.CSS("background: "+h))
		}
	}
	return out
}

// Style is the look as custom properties for a block that wears it on
// any page (a songbook's banner): --look-bg, --look-header, --look-fg,
// --look-muted, --look-accent, --look-on-accent, --look-accent-2 and
// --look-font. Every color is a hex color Derive made, the font a stack
// of the curated list.
func (d Derived) Style() template.CSS {
	b := d.Brand
	if b.Accent == "" {
		return ""
	}
	css := fmt.Sprintf("--look-bg: %s; --look-header: %s; --look-fg: %s; --look-muted: %s; --look-accent: %s; --look-on-accent: %s; --look-accent-2: %s",
		b.Background, b.Header, b.Text, b.Muted, b.Accent, contrastOn(b.Accent), b.Accent2)
	if f, ok := Fonts[b.FontDisplay]; ok {
		css += "; --look-font: " + f.Stack
	}
	return template.CSS(css)
}

// FontsURL is the Google Fonts stylesheet of the look's title font, ""
// when it needs none.
func (d Derived) FontsURL() string { return fontsURL(d.FontDisplay) }

// Derive makes a band's look from the covers of its songs, the way a
// player tints its screen from the album playing, but for a whole
// repertoire: the covers' strongest hue is the accent, the next distinct
// one the second accent, their average lightness says dark or light, the
// neutrals are tinted with the accent's hue, and the genres pick the
// fonts and the shape. Every pair the app puts together (text on the
// page, white or black on an accent button, the tagline on the header)
// reads at WCAG AA: lightness is moved until it does.
func Derive(songs []Song) Derived {
	type sample struct{ l, c, h, w float64 }
	var all []sample
	n := 0
	for _, s := range songs {
		if len(s.Colors) > 0 {
			n++
		}
	}
	if n == 0 {
		return Derived{}
	}
	var meanL, wsum float64
	for _, s := range songs {
		for _, c := range s.Colors {
			if !hexColor.MatchString(c.Hex) {
				continue
			}
			l, ch, h := oklch(c.Hex)
			w := c.Share / float64(n)
			all = append(all, sample{l, ch, h, w})
			meanL += l * w
			wsum += w
		}
	}
	if wsum == 0 {
		return Derived{}
	}
	meanL /= wsum

	// The hues, in 24 bins of 15°, weighed by how much and how colorful;
	// greys don't vote.
	const bins = 24
	var hist [bins]float64
	for _, s := range all {
		if s.c < 0.04 {
			continue
		}
		hist[int(s.h/15)%bins] += s.w * s.c
	}
	smooth := func(i int) float64 {
		return hist[(i+bins-1)%bins]*0.5 + hist[i] + hist[(i+1)%bins]*0.5
	}
	order := make([]int, bins)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return smooth(order[a]) > smooth(order[b]) })
	// The mean hue and chroma of the samples within 30° of a bin.
	around := func(bin int) (h, c float64) {
		center := float64(bin)*15 + 7.5
		var x, y, cw, w float64
		for _, s := range all {
			if s.c < 0.04 || hueDist(s.h, center) > 30 {
				continue
			}
			k := s.w * s.c
			x += math.Cos(s.h*math.Pi/180) * k
			y += math.Sin(s.h*math.Pi/180) * k
			cw += s.c * s.w
			w += s.w
		}
		if w == 0 {
			return center, 0.12
		}
		return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360), cw / w
	}
	h1, c1 := around(order[0])
	h2, c2 := h1+40, c1*0.8 // no second hue: a neighbour of the first
	for _, b := range order[1:] {
		if smooth(b) < smooth(order[0])*0.2 {
			break
		}
		if hh, cc := around(b); hueDist(hh, h1) >= 60 {
			h2, c2 = hh, cc
			break
		}
	}
	c1 = clamp(c1*1.25, 0.09, 0.2)
	c2 = clamp(c2*1.25, 0.08, 0.18)

	d := Derived{Mood: "light", Hue: h1}
	if meanL < 0.5 {
		d.Mood = "dark"
	}
	b := Brand{}
	if d.Mood == "dark" {
		b.Background = lch(0.19, 0.022, h1)
		b.Surface = lch(0.23, 0.026, h1)
		b.Header = lch(0.15, 0.03, h1)
		b.Text = lch(0.96, 0.008, h1)
		b.Muted = lch(0.78, 0.025, h1)
		b.Accent = readable(0.72, c1, h1, +1, b.Background)
		b.Accent2 = readable(0.7, c2, h2, +1, b.Header)
	} else {
		b.Background = lch(0.975, 0.012, h1)
		b.Surface = lch(0.995, 0.004, h1)
		b.Header = lch(0.3, 0.05, h1)
		b.Text = lch(0.22, 0.02, h1)
		b.Muted = lch(0.47, 0.03, h1)
		b.Accent = readable(0.55, c1, h1, -1, b.Background)
		b.Accent2 = readable(0.78, c2, h2, +1, b.Header)
	}

	// The genres: the most frequent picks the fonts and the shape.
	count := map[string]int{}
	for _, s := range songs {
		if g := strings.TrimSpace(s.Genre); g != "" {
			count[g]++
		}
	}
	for g := range count {
		d.Genres = append(d.Genres, g)
	}
	sort.Slice(d.Genres, func(i, j int) bool {
		if count[d.Genres[i]] != count[d.Genres[j]] {
			return count[d.Genres[i]] > count[d.Genres[j]]
		}
		return d.Genres[i] < d.Genres[j]
	})
	family := map[string]int{}
	for g, k := range count {
		family[genreFamily(g)] += k
	}
	best, bestN := "", 0
	for f, k := range family {
		if k > bestN || k == bestN && f < best {
			best, bestN = f, k
		}
	}
	look := genreLooks[best]
	b.FontDisplay, b.FontUI, b.Shape = look[0], look[1], look[2]
	d.Brand = b

	for _, bin := range order[:6] {
		if smooth(bin) == 0 {
			break
		}
		h, c := around(bin)
		d.Hues = append(d.Hues, lch(0.62, clamp(c*1.2, 0.06, 0.2), h))
	}
	return d
}

// genreLooks: display font, text font and shape, by family of genres.
var genreLooks = map[string][3]string{
	"blues": {"fraunces", "work-sans", "soft"},
	"rock":  {"anton", "inter", "square"},
	"pop":   {"space-grotesk", "inter", "pill"},
	"fr":    {"playfair", "work-sans", "soft"},
	"":      {"oswald", "inter", "soft"},
}

func genreFamily(g string) string {
	g = strings.ToLower(g)
	switch {
	case strings.Contains(g, "fran") || strings.Contains(g, "chanson") || strings.Contains(g, "variété"):
		return "fr"
	case strings.Contains(g, "blues") || strings.Contains(g, "soul") || strings.Contains(g, "r&b") || strings.Contains(g, "jazz") || strings.Contains(g, "gospel") || strings.Contains(g, "country") || strings.Contains(g, "folk") || strings.Contains(g, "singer"):
		return "blues"
	case strings.Contains(g, "rock") || strings.Contains(g, "metal") || strings.Contains(g, "punk") || strings.Contains(g, "alternative") || strings.Contains(g, "grunge"):
		return "rock"
	case strings.Contains(g, "pop") || strings.Contains(g, "dance") || strings.Contains(g, "electro") || strings.Contains(g, "disco") || strings.Contains(g, "funk") || strings.Contains(g, "hip") || strings.Contains(g, "reggae"):
		return "pop"
	}
	return ""
}

// readable is the color of hue h and chroma c nearest lightness l that
// reads at 4.5:1 on bg and under its own button text (contrastOn), moving
// lightness by dir (+1 lighter, −1 darker) until it does.
func readable(l, c, h float64, dir float64, bg string) string {
	for i := 0; i < 60; i++ {
		hex := lch(l, c, h)
		if contrast(hex, bg) >= 4.5 && contrast(hex, contrastOn(hex)) >= 4.5 {
			return hex
		}
		l = clamp(l+dir*0.01, 0.05, 0.98)
	}
	return lch(l, c, h)
}

// contrast is the WCAG contrast ratio of two hex colors.
func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func clamp(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }

func hueDist(a, b float64) float64 {
	d := math.Abs(a - b)
	return math.Min(d, 360-d)
}

// oklch is a hex color in OKLCH: lightness 0–1, chroma, hue in degrees.
func oklch(hex string) (l, c, h float64) {
	r, g, b := rgb(hex)
	lin := func(x float64) float64 {
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	r, g, b = lin(r), lin(g), lin(b)
	lm := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	L := 0.2104542553*lm + 0.7936177850*m - 0.0040720468*s
	A := 1.9779984951*lm - 2.4285922050*m + 0.4505937099*s
	B := 0.0259040371*lm + 0.7827717662*m - 0.8086757660*s
	return L, math.Hypot(A, B), math.Mod(math.Atan2(B, A)*180/math.Pi+360, 360)
}

// lch is an OKLCH color as hex, its chroma reduced until it fits sRGB.
func lch(l, c, h float64) string {
	for ; c >= 0; c -= 0.005 {
		if hex, ok := fromOKLCH(l, c, h); ok {
			return hex
		}
	}
	hex, _ := fromOKLCH(l, 0, h)
	return hex
}

func fromOKLCH(L, C, h float64) (string, bool) {
	A, B := C*math.Cos(h*math.Pi/180), C*math.Sin(h*math.Pi/180)
	lm := math.Pow(L+0.3963377774*A+0.2158037573*B, 3)
	m := math.Pow(L-0.1055613458*A-0.0638541728*B, 3)
	s := math.Pow(L-0.0894841775*A-1.2914855480*B, 3)
	lin := [3]float64{
		4.0767416621*lm - 3.3077115913*m + 0.2309699292*s,
		-1.2684380046*lm + 2.6097574011*m - 0.3413193965*s,
		-0.0041960863*lm - 0.7034186147*m + 1.7076147010*s,
	}
	var out [3]int
	for i, x := range lin {
		if x < -0.0005 || x > 1.0005 {
			return "", false
		}
		x = clamp(x, 0, 1)
		if x <= 0.0031308 {
			x *= 12.92
		} else {
			x = 1.055*math.Pow(x, 1/2.4) - 0.055
		}
		out[i] = int(x*255 + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2]), true
}
