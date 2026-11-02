package pdfnative

// rgb is a small (r,g,b uint8) tuple — gopdf's color setters take three
// separate bytes, not a struct, but passing them around as one value keeps
// the theme tables below readable.
type rgb struct{ r, g, b uint8 }

// theme mirrors v2/lib/theme.typ's LIGHT/DARK palettes (the Typst pipeline
// this package replaces) so a page rendered natively reads the same as one
// rendered through Typst ever did — same ink/chord/chip colors, just drawn
// by a different engine.
type theme struct {
	bg       rgb // page background (DARK only; LIGHT pages are plain white)
	ink      rgb // body text
	chord    rgb // chord symbols
	faint    rgb // secondary/meta text
	chip     rgb // section-tag chip background
	chipInk  rgb // section-tag chip text
	meta     rgb // contents-table meta columns
	line     rgb // rules/dividers
	badge    rgb // rehearsal-mark letter badge background (iReal Pro style)
	badgeInk rgb // rehearsal-mark letter badge text
}

var lightTheme = theme{
	bg:       rgb{255, 255, 255},
	ink:      rgb{0x14, 0x14, 0x14},
	chord:    rgb{0x8a, 0x1c, 0x1c},
	faint:    rgb{0x9a, 0x9a, 0x9a},
	chip:     rgb{0xe8, 0xe8, 0xe8},
	chipInk:  rgb{0x14, 0x14, 0x14},
	meta:     rgb{0x5a, 0x5a, 0x5a},
	line:     rgb{0x14, 0x14, 0x14},
	badge:    rgb{0xc0, 0x39, 0x2b},
	badgeInk: rgb{255, 255, 255},
}

var darkTheme = theme{
	bg:       rgb{0x0f, 0x0f, 0x0f},
	ink:      rgb{0xed, 0xed, 0xed},
	chord:    rgb{0xe8, 0xa3, 0x3d},
	faint:    rgb{0x6e, 0x6e, 0x6e},
	chip:     rgb{0x2b, 0x2b, 0x2b},
	chipInk:  rgb{0xed, 0xed, 0xed},
	meta:     rgb{0x9a, 0x9a, 0x9a},
	line:     rgb{0xb4, 0xb4, 0xb4},
	badge:    rgb{0xe0, 0x55, 0x45},
	badgeInk: rgb{0x0f, 0x0f, 0x0f},
}

func themeFor(name string) theme {
	if name == "dark" {
		return darkTheme
	}
	return lightTheme
}

// Cover/title-plate palette (Phase 33's Typst front matter): always a dark
// full-bleed plate regardless of the body theme — a cover page is a cover
// page whether the songs inside print light or dark.
var (
	plateBg     = rgb{0x0f, 0x0f, 0x0f}
	plateInk    = rgb{255, 255, 255}
	plateMeta   = rgb{0x9a, 0x9a, 0x9a}
	accentAmber = rgb{0xe8, 0xa3, 0x3d}
)

// Page geometry: A4 portrait in points, matching v2/lib/theme.typ's "a4"
// TARGET (the only size this app's export routes ever request).
const (
	pageW   = 595.28
	pageH   = 841.89
	marginX = 40.0
	marginY = 44.0
)

func contentWidth() float64  { return pageW - 2*marginX }
func contentBottom() float64 { return pageH - marginY }
