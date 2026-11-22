package httpapi

import (
	"net/url"
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
	// Name is the reference's name beside its thumbnail: the app keeps no
	// video title, so the link's site ("YouTube", "open.spotify.com"); a
	// band recording is named by the template.
	Name string
	// Title is the video's or page's own title when it was fetched
	// (reference_title.go), shown in place of Name.
	Title   string
	CanEdit bool
}

// A YouTube video id as it goes into the thumbnail's address.
var youtubeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{6,20}$`)

func songRefHeadFor(reference string, hasBandRecording, canEdit bool) songRefHead {
	h := songRefHead{CanEdit: canEdit}
	reference = strings.TrimSpace(reference)
	switch {
	case reference != "":
		h.Kind, h.Name = "link", refLinkName(reference)
		if id := youtubeID(reference); youtubeIDRe.MatchString(id) {
			h.Kind, h.YouTubeID, h.Name = "youtube", id, "YouTube"
		}
	case hasBandRecording:
		h.Kind = "band"
	}
	return h
}

// refLinkName is a link's site, for its name: its host without "www." or
// "m."; YouTube's, "YouTube". The link itself when it has no host.
func refLinkName(ref string) string {
	u, err := url.Parse(strings.TrimSpace(ref))
	if err != nil || u.Hostname() == "" {
		return strings.TrimSpace(ref)
	}
	host := strings.ToLower(u.Hostname())
	for _, p := range []string{"www.", "m."} {
		host = strings.TrimPrefix(host, p)
	}
	if host == "youtube.com" || host == "youtu.be" || host == "music.youtube.com" {
		return "YouTube"
	}
	return host
}
