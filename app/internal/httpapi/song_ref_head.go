package httpapi

import (
	"regexp"
	"strings"
)

// songRefHead is the song's reference in its header's meta line (play.html
// "song-ref-head"): after the artist, a small thumbnail of the YouTube
// video, or an icon, and "Reference"; a click opens the Reference view
// (the video embedded, the recordings), the one the ⋯ menu opens. With no
// reference, the song's editors get "+ Reference", which opens that
// view's link field; others see nothing.
type songRefHead struct {
	// Kind is "youtube" (a YouTube link: its thumbnail), "link" (any
	// other link), "band" (no link, a band recording) or "" (none).
	Kind      string
	YouTubeID string
	CanEdit   bool
}

// A YouTube video id as it goes into the thumbnail's address.
var youtubeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)

func songRefHeadFor(reference string, hasBandRecording, canEdit bool) songRefHead {
	h := songRefHead{CanEdit: canEdit}
	reference = strings.TrimSpace(reference)
	switch {
	case reference != "":
		h.Kind = "link"
		if id := youtubeID(reference); youtubeIDRe.MatchString(id) {
			h.Kind, h.YouTubeID = "youtube", id
		}
	case hasBandRecording:
		h.Kind = "band"
	}
	return h
}
