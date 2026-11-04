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

	// Style knobs below this line are NOT part of the original light/dark
	// pair (both default to the same values light/dark always drew) —
	// added for the "classic"/"stage" alternative styles, see each
	// variant's own doc comment for what real-world chart style it's
	// drawing from.
	titleFont    string  // faceSerif/faceSansBold/etc — see fonts.go
	titleSize    float64 // pt, at the fixed (non-shrink-to-fit) header size
	titleUpper   bool    // small-caps-ish feel on a face with no real small caps: just upper the title
	ruleWeight   float64 // header-rule stroke width
	doubleRule   bool    // a second, thinner rule a couple points below the main one
	badgeRound   bool    // rehearsal-mark badge drawn as a circle instead of iReal's square
	badgeOutline bool    // badge drawn as an outline (ink-colored ring) instead of a filled chip — restrained, Real-Book-ish
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
	titleFont: faceSerif, titleSize: 20, ruleWeight: 1.1,
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
	titleFont: faceSerif, titleSize: 20, ruleWeight: 1.1,
}

// classicTheme — "Real Book" style: restrained, mostly monochrome,
// hand-engraved fake-book feel. Chords are ink-black, not a separate
// accent color (a legal Real Book prints everything in one color — the
// chord symbol's bold weight alone is what sets it apart from the lyric,
// not a hue); section marks are a plain outlined circle, no filled chip,
// matching how a real fake book marks a rehearsal letter: a small ringed
// letter, not a colored UI badge. A double rule under the title is the
// one deliberately old-fashioned, non-"app" flourish — classic sheet
// music title pages often use a hairline-plus-rule pair under the title
// block.
var classicTheme = theme{
	bg:       rgb{255, 255, 255},
	ink:      rgb{0x1a, 0x1a, 0x1a},
	chord:    rgb{0x1a, 0x1a, 0x1a},
	faint:    rgb{0x6a, 0x6a, 0x6a},
	chip:     rgb{255, 255, 255},
	chipInk:  rgb{0x1a, 0x1a, 0x1a},
	meta:     rgb{0x4a, 0x4a, 0x4a},
	line:     rgb{0x1a, 0x1a, 0x1a},
	badge:    rgb{255, 255, 255},
	badgeInk: rgb{0x1a, 0x1a, 0x1a},
	titleFont: faceSerif, titleSize: 24, titleUpper: false,
	ruleWeight: 0.9, doubleRule: true, badgeRound: true, badgeOutline: true,
}

// stageTheme — "modern stage" style, the iReal Pro / OnSong end of the
// spectrum: bold, high-contrast, built to be glanced at from arm's length
// under stage lighting rather than read closely at a desk. Bigger title,
// a vivid single accent color used hard (chords AND the rehearsal badge
// both carry it, not just the badge), no double rule — one confident
// heavy rule instead.
var stageTheme = theme{
	bg:       rgb{255, 255, 255},
	ink:      rgb{0x11, 0x11, 0x11},
	chord:    rgb{0x1e, 0x5f, 0x9e},
	faint:    rgb{0x8a, 0x8a, 0x8a},
	chip:     rgb{0x1e, 0x5f, 0x9e},
	chipInk:  rgb{255, 255, 255},
	meta:     rgb{0x55, 0x55, 0x55},
	line:     rgb{0x1e, 0x5f, 0x9e},
	badge:    rgb{0x1e, 0x5f, 0x9e},
	badgeInk: rgb{255, 255, 255},
	titleFont: faceSansBold, titleSize: 26, titleUpper: true,
	ruleWeight: 2.2, doubleRule: false, badgeRound: false, badgeOutline: false,
}

func themeFor(name string) theme {
	switch name {
	case "dark":
		return darkTheme
	case "classic":
		return classicTheme
	case "stage":
		return stageTheme
	default:
		return lightTheme
	}
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
