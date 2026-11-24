// Package pdfnative is the app's pure-Go, in-process PDF renderer for
// songs, songbooks and setlists — no `typst`/`lilypond` binary, no /v2
// bind mount, just this package.
//
// It never shells out to anything: it parses a song's body with
// internal/song (the same parser the editor already uses) and draws
// directly onto a github.com/signintech/gopdf document with embedded,
// go:embed-bundled fonts — consistent with this app's established
// offline-capable philosophy (no network font/binary fetches at runtime).
package pdfnative

import (
	_ "embed"

	"github.com/signintech/gopdf"
)

//go:embed fonts/EBGaramond.ttf
var fontSerif []byte

//go:embed fonts/EBGaramond-Italic.ttf
var fontSerifItalic []byte

//go:embed fonts/Lato-Regular.ttf
var fontSans []byte

//go:embed fonts/Lato-Bold.ttf
var fontSansBold []byte

//go:embed fonts/Lato-Italic.ttf
var fontSansItalic []byte

// The Jazz chart's two faces, the ones its web page draws with
// (static/css/jazz.css), as TrueType: the woff2 files the page serves,
// decompressed (woff2_decompress) — gopdf reads TTF only, and embeds a
// subset of the glyphs a document uses. Both by Steinberg under the SIL
// Open Font License 1.1 (fonts/Petaluma-OFL.txt), which allows embedding
// them in documents: Petaluma Script unmodified, and ChartSymbols, the
// page's renamed subset of Petaluma (SMuFL accidentals, chord-quality
// signs, simile, segno, coda, whole rest, tempo note and dot).
//
//go:embed fonts/PetalumaScript.ttf
var fontHand []byte

//go:embed fonts/ChartSymbols.ttf
var fontMusic []byte

const (
	faceSerif    = "serif"     // EB Garamond — titles, the cover plate
	faceSerifIt  = "serif-it"  // EB Garamond italic — artist bylines
	faceSans     = "sans"      // Lato — lyrics, meta text, contents table
	faceSansBold = "sans-bold" // Lato Bold — chord symbols, section tags, headings
	faceSansIt   = "sans-it"   // Lato Italic — secondary meta
	faceHand     = "jz-hand"   // Petaluma Script — the Jazz chart's lettering
	faceMusic    = "jz-music"  // ChartSymbols (Petaluma) — the Jazz chart's music signs
)

// registerFonts loads every embedded face into doc, once per document.
func registerFonts(doc *gopdf.GoPdf) error {
	for _, f := range []struct {
		name string
		data []byte
	}{
		{faceSerif, fontSerif},
		{faceSerifIt, fontSerifItalic},
		{faceSans, fontSans},
		{faceSansBold, fontSansBold},
		{faceSansIt, fontSansItalic},
		{faceHand, fontHand},
		{faceMusic, fontMusic},
	} {
		if err := doc.AddTTFFontData(f.name, f.data); err != nil {
			return err
		}
	}
	return nil
}
