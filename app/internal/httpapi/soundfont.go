package httpapi

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"app/internal/soundfont"
	"app/internal/webassets"
)

// SoundFonts makes the small soundfonts a score plays with: GeneralUser GS
// (docs/soundfont.md), cut down to the presets the score uses. Set by main
// when the bank is there; nil, pages play with alphaTab's Sonivox.
var SoundFonts *soundfont.Subsets

func init() {
	// The pages' scripts (gp-soundfont.js) are told which bank, if any.
	webassets.SoundfontID = func() string { return SoundFonts.ID() }
}

// UseSoundFont serves subsets of the bank at path, cached in dir up to
// maxBytes; the bank is read (and hashed) now, in the background, and the
// log says which it is.
func UseSoundFont(path, dir string, maxBytes int64) {
	SoundFonts = soundfont.NewSubsets(path, dir, maxBytes)
	go LogSoundFont()
}

// LogSoundFont says at startup which bank scores play with.
func LogSoundFont() {
	if SoundFonts == nil {
		log.Printf("soundfont: none configured; scores play with Sonivox")
		return
	}
	if err := SoundFonts.Err(); err != nil {
		log.Printf("soundfont: %v; scores play with Sonivox", err)
		return
	}
	log.Printf("soundfont: %s (%s)", SoundFonts.Name(), SoundFonts.ID())
}

// handleSoundFontSubset: GET /soundfonts/subset/{key}.sf2?sf={id}, the
// bank's presets named in key (soundfont.Key: "0-24_0-33_128-0"), made on
// first use and cached on disk. The URL names its content (the key, the
// bank's id), so it is cached for a year; another spelling of the same
// key, or another bank's id (a page from before the bank changed), is
// sent to the right URL.
func (s *Server) handleSoundFontSubset(w http.ResponseWriter, r *http.Request) {
	id := SoundFonts.ID()
	key, ok := strings.CutSuffix(r.PathValue("file"), ".sf2")
	if id == "" || !ok {
		http.NotFound(w, r)
		return
	}
	presets, err := soundfont.ParseKey(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if canon := soundfont.Key(presets); canon != key || r.URL.Query().Get("sf") != id {
		w.Header().Set("Cache-Control", "no-cache")
		http.Redirect(w, r, "/soundfonts/subset/"+canon+".sf2?sf="+url.QueryEscape(id), http.StatusFound)
		return
	}
	path, etag, err := SoundFonts.Get(r.Context(), presets)
	if err != nil {
		log.Printf("soundfont subset %s: %v", key, err)
		http.Error(w, "could not make the soundfont", http.StatusInternalServerError)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "could not read the soundfont", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "audio/x-soundfont")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	http.ServeContent(w, r, "", time.Time{}, f)
}
