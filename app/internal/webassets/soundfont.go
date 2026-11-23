package webassets

// SoundfontID is set by the server (httpapi.SoundFonts): the id of the
// bank whose subsets scores play with, "" for none (docs/soundfont.md).
var SoundfontID func() string

// {{soundfontID}}: that id, for gp-soundfont.js's script tag. Added to
// funcMap as a package variable is initialized, before init() parses the
// templates with it.
var _ = func() bool {
	funcMap["soundfontID"] = func() string {
		if SoundfontID == nil {
			return ""
		}
		return SoundfontID()
	}
	return true
}()
