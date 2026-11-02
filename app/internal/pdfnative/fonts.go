// Package pdfnative is a pure-Go, in-process PDF renderer for songs,
// songbooks and setlists — a drop-in replacement for internal/pdf's
// Typst-shelling Bridge (see pdf.Exporter), built so the app no longer
// needs the `typst` binary or a /v2 bind mount just to export a PDF.
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

const (
	faceSerif    = "serif"     // EB Garamond — titles, cover/title plates
	faceSerifIt  = "serif-it"  // EB Garamond italic — artist bylines
	faceSans     = "sans"      // Lato — lyrics, meta text, contents table
	faceSansBold = "sans-bold" // Lato Bold — chord symbols, section tags, headings
	faceSansIt   = "sans-it"   // Lato Italic — secondary meta
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
	} {
		if err := doc.AddTTFFontData(f.name, f.data); err != nil {
			return err
		}
	}
	return nil
}
